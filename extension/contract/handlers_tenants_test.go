package contract

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
)

func mustDispatch(t *testing.T, d *dispatcher.Dispatcher, intent string, in any, kind dash.Kind) map[string]any {
	t.Helper()
	payload, err := json.Marshal(in)
	if err != nil {
		t.Fatal("marshal request")
	}
	b, err := dispatch(t, d, intent, string(payload), kind)
	if err != nil {
		t.Fatalf("%s: %v", intent, err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal("decode response")
	}
	return out
}

func TestTenantCommandsPreserveUnspecifiedFields(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		gw := testGateway(t, nexus.WithDatabase(s))
		d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
		created := mustDispatch(t, d, "tenants.create", map[string]any{
			"name": "One", "slug": "one",
			"quota":    map[string]any{"rpm": 10, "maxStreamTokens": 900, "maxStreamDurationMs": 60000, "monthlyBudgetUsd": "1284.370219"},
			"config":   map[string]any{"allowedModels": []string{"gpt-4o"}, "cacheEnabled": false, "metadata": map[string]string{"region": "west"}},
			"metadata": map[string]string{"owner": "ops"},
		}, dash.KindCommand)
		tid := created["id"].(string)
		if _, err := s.Tenants().FindByID(context.Background(), tid); err != nil {
			t.Fatalf("read created tenant: %v", err)
		}
		mustDispatch(t, d, "tenants.update", map[string]any{"id": tid, "quota": map[string]int{"rpm": 25}}, dash.KindCommand)
		got, err := s.Tenants().FindByID(context.Background(), tid)
		if err != nil {
			t.Fatal(err)
		}
		if got.Quota.RPM != 25 || got.Quota.MaxStreamTokens != 900 || got.Quota.MaxStreamDuration.Milliseconds() != 60000 || got.Quota.MonthlyBudgetUSD.String() != "1284.370219" {
			t.Fatal("quota patch erased populated fields")
		}
		if got.Config.CacheEnabled == nil || *got.Config.CacheEnabled || got.Config.Metadata["region"] != "west" || got.Metadata["owner"] != "ops" {
			t.Fatal("patch erased config or metadata")
		}
		mustDispatch(t, d, "tenants.update", map[string]any{"id": tid, "config": map[string]any{"cacheEnabled": nil}}, dash.KindCommand)
		got, err = s.Tenants().FindByID(context.Background(), tid)
		if err != nil || got.Config.CacheEnabled != nil || len(got.Config.AllowedModels) != 1 {
			t.Fatal("cache reset erased unrelated fields")
		}
		_, err = dispatch(t, d, "tenants.create", `{"name":"duplicate","slug":"one"}`, dash.KindCommand)
		if codeOf(err) != dash.CodeConflict {
			t.Fatal("duplicate slug must conflict")
		}
	})
}

func TestTenantPagingFiltersAndUnavailableSpend(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		gw := testGateway(t, nexus.WithUsageEnabled(false), nexus.WithDatabase(s))
		d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
		one := mustDispatch(t, d, "tenants.create", map[string]string{"name": "One", "slug": "one"}, dash.KindCommand)
		two := mustDispatch(t, d, "tenants.create", map[string]string{"name": "Two", "slug": "two"}, dash.KindCommand)
		mustDispatch(t, d, "tenants.setStatus", map[string]any{"id": one["id"], "status": "suspended"}, dash.KindCommand)
		filtered := mustDispatch(t, d, "tenants.list", map[string]any{"status": "suspended", "search": "ON"}, dash.KindQuery)
		items := filtered["items"].([]any)
		if len(items) != 1 || items[0].(map[string]any)["id"] != one["id"] {
			t.Fatal("tenant filters returned wrong identities")
		}
		first := mustDispatch(t, d, "tenants.list", map[string]any{"limit": 1}, dash.KindQuery)
		if first["nextCursor"] == "" {
			t.Fatal("missing continuation")
		}
		second := mustDispatch(t, d, "tenants.list", map[string]any{"limit": 1, "cursor": first["nextCursor"]}, dash.KindQuery)
		if first["items"].([]any)[0].(map[string]any)["id"] != two["id"] || second["items"].([]any)[0].(map[string]any)["id"] != one["id"] || second["nextCursor"] != "" {
			t.Fatal("paging skipped or repeated a tenant")
		}
		detail := mustDispatch(t, d, "tenants.get", map[string]any{"id": one["id"]}, dash.KindQuery)
		if detail["monthSpendUsd"] != nil || detail["requestsToday"] != nil || detail["usageEnabled"] != false {
			t.Fatal("disabled usage appeared measured")
		}
	})
}

func TestTenantValidation(t *testing.T) {
	gw := testGateway(t)
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	for _, payload := range []string{
		`{"name":" ","slug":"x"}`, `{"name":"x","slug":" "}`,
		`{"name":"x","slug":"x","quota":{"monthlyBudgetUsd":"-1"}}`,
		`{"name":"x","slug":"x","quota":{"monthlyBudgetUsd":1.2}}`,
		`{"name":"x","slug":"x","quota":{"maxStreamDurationMs":9223372036854775807}}`,
		`{"name":"x","slug":"x","quota":{"rpm":-1}}`,
		`{"name":"x","slug":"x","config":{"allowedModels":["a"],"blockedModels":["a"]}}`,
	} {
		_, err := dispatch(t, d, "tenants.create", payload, dash.KindCommand)
		if codeOf(err) != dash.CodeBadRequest {
			t.Errorf("expected bad request for %s, got %s", payload, codeOf(err))
		}
	}
	for _, tc := range []struct {
		id   string
		code dash.ErrorCode
	}{{"bad", dash.CodeBadRequest}, {id.NewTenantID().String(), dash.CodeNotFound}} {
		_, err := dispatch(t, d, "tenants.get", fmt.Sprintf(`{"id":%q}`, tc.id), dash.KindQuery)
		if codeOf(err) != tc.code {
			t.Errorf("get error=%s want=%s", codeOf(err), tc.code)
		}
	}
	_, err := dispatch(t, d, "tenants.list", `{"cursor":"invalid"}`, dash.KindQuery)
	if codeOf(err) != dash.CodeBadRequest {
		t.Fatal("invalid cursor accepted")
	}
}
