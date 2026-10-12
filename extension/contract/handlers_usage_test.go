package contract

import (
	"context"
	"fmt"
	"testing"
	"time"

	dash "github.com/xraph/forge/extensions/dashboard/contract"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/store/storetest"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

func TestUsageRecordsProjectLabels(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		tn := storetest.InsertTenant(t, s)
		gw := testGateway(t, nexus.WithDatabase(s))
		k, _, err := gw.Keys().Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "request log"})
		if err != nil {
			t.Fatal(err)
		}
		r := storetest.Record(tn.ID, "0.001")
		r.KeyID = k.ID
		storetest.InsertRecord(t, s, r)
		d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
		out := mustDispatch(t, d, "usage.records", map[string]any{}, dash.KindQuery)
		rows := out["items"].([]any)
		if len(rows) != 1 {
			t.Fatal("request log lost its record")
		}
		row := rows[0].(map[string]any)
		if row["tenantName"] != tn.Name || row["keyPrefix"] != k.Prefix {
			t.Fatal("request log omitted current display labels")
		}
	})
}

func TestUsageRecordsKeepHistoricalRowsWithoutResources(t *testing.T) {
	gw := testGateway(t)
	r := storetest.Record(id.NewTenantID(), "0")
	r.KeyID = id.NewKeyID()
	if err := gw.Usage().Record(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	out := mustDispatch(t, d, "usage.records", map[string]any{}, dash.KindQuery)
	rows := out["items"].([]any)
	if len(rows) != 1 {
		t.Fatal("missing resource removed historical usage")
	}
	row := rows[0].(map[string]any)
	if row["tenantName"] != nil || row["keyPrefix"] != nil {
		t.Fatal("historical row fabricated missing labels")
	}
}

func TestUsageQueriesKeepExactCostScopeAndPaging(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		one, two := storetest.InsertTenant(t, s), storetest.InsertTenant(t, s)
		gw := testGateway(t, nexus.WithDatabase(s))
		d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
		records := []*usage.Record{storetest.Record(one.ID, "0.1"), storetest.Record(one.ID, "0.2"), storetest.Record(one.ID, ""), storetest.Record(one.ID, "0"), storetest.Record(one.ID, "0"), storetest.Record(two.ID, "9"), storetest.Record(one.ID, "40")}
		records[0].Provider = "a"
		records[1].Provider = "z"
		records[3].Cached = true
		records[3].Outcome = usage.OutcomeCached
		records[3].PricingStatus = usage.PricingCached
		records[4].Outcome = usage.OutcomeRefused
		records[4].PricingStatus = usage.PricingNotCharged
		records[4].RefusalCode = "rpm"
		records[6].CreatedAt = time.Now().UTC().AddDate(0, -2, 0)
		for _, r := range records {
			r.KeyID = id.Nil
			storetest.InsertRecord(t, s, r)
		}
		summary := mustDispatch(t, d, "usage.summary", map[string]string{"tenantId": one.ID.String(), "period": "month"}, dash.KindQuery)
		if summary["totalCostUsd"] != "0.3" || summary["totalRequests"] != float64(5) || summary["unpricedRequests"] != float64(1) || summary["avgLatencyMs"] != float64(1500) {
			t.Fatalf("unexpected summary: %v", summary)
		}
		byProvider := summary["byProvider"].([]any)
		if byProvider[0].(map[string]any)["name"] != "z" || byProvider[1].(map[string]any)["name"] != "a" {
			t.Fatal("aggregates not ordered by exact cost")
		}
		all := mustDispatch(t, d, "usage.summary", map[string]string{"period": "month"}, dash.KindQuery)
		if all["totalCostUsd"] != "9.3" {
			t.Fatal("operator summary did not include both tenants")
		}
		overview := mustDispatch(t, d, "overview.get", map[string]any{}, dash.KindQuery)
		if overview["monthSpendUsd"] != "9.3" || overview["requestsToday"] != float64(5) || overview["unpricedRequests"] != float64(1) {
			t.Fatal("overview lost exact spend or counted refusals against daily requests")
		}
		series := mustDispatch(t, d, "usage.series", map[string]string{"tenantId": one.ID.String(), "period": "month", "bucket": "day"}, dash.KindQuery)
		found := false
		for _, raw := range series["items"].([]any) {
			p := raw.(map[string]any)
			if p["requests"] == float64(5) {
				found = p["costUsd"] == "0.3"
			}
		}
		if !found {
			t.Fatal("series lost the exact bucket cost")
		}
		seen := map[string]bool{}
		cursor := ""
		unknown := false
		for {
			page := mustDispatch(t, d, "usage.records", map[string]any{"tenantId": one.ID.String(), "from": time.Now().UTC().Add(-time.Hour).Format(time.RFC3339), "limit": 2, "cursor": cursor}, dash.KindQuery)
			for _, raw := range page["items"].([]any) {
				r := raw.(map[string]any)
				rid := r["id"].(string)
				if seen[rid] || r["tenantId"] != one.ID.String() {
					t.Fatal("paging repeated or escaped scope")
				}
				seen[rid] = true
				if r["pricingStatus"] == "unpriced_model" {
					unknown = r["costUsd"] == nil
				}
				if r["latencyMs"] != float64(1500) {
					t.Fatal("latency is not milliseconds")
				}
			}
			cursor = page["nextCursor"].(string)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 5 || !unknown {
			t.Fatal("page traversal lost records or unknown pricing")
		}
		filtered := mustDispatch(t, d, "usage.records", map[string]string{"tenantId": one.ID.String(), "outcome": "refused"}, dash.KindQuery)
		if len(filtered["items"].([]any)) != 1 {
			t.Fatal("outcome filter ignored")
		}
	})
}

func TestUsageValidationAndDisabledCollection(t *testing.T) {
	gw := testGateway(t, nexus.WithUsageEnabled(false))
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	for _, intent := range []string{"usage.summary", "usage.series", "usage.records"} {
		for _, raw := range []string{`null`, `""`, `" "`, `7`, `{}`, `"bad"`} {
			_, err := dispatch(t, d, intent, `{"tenantId":`+raw+`,"period":"month","bucket":"day"}`, dash.KindQuery)
			if codeOf(err) != dash.CodeBadRequest {
				t.Fatalf("%s accepted unusable scope %s", intent, raw)
			}
		}
		_, err := dispatch(t, d, intent, fmt.Sprintf(`{"tenantId":%q,"period":"month","bucket":"day"}`, id.NewTenantID().String()), dash.KindQuery)
		if codeOf(err) != dash.CodeNotFound {
			t.Fatal("unknown tenant widened scope")
		}
	}
	for _, tc := range []struct{ intent, payload string }{
		{"usage.summary", `{"period":"year"}`}, {"usage.series", `{"period":"month","bucket":"second"}`},
		{"usage.records", `{"from":"bad"}`}, {"usage.records", `{"from":"2026-10-08T00:00:00Z","to":"2026-10-07T00:00:00Z"}`},
		{"usage.records", `{"keyId":null}`}, {"usage.records", `{"outcome":"made_up"}`}, {"usage.records", `{"cursor":"bad"}`},
	} {
		_, err := dispatch(t, d, tc.intent, tc.payload, dash.KindQuery)
		if codeOf(err) != dash.CodeBadRequest {
			t.Errorf("validation: %s %s = %s", tc.intent, tc.payload, codeOf(err))
		}
	}
	for _, intent := range []string{"usage.summary", "usage.series", "usage.records"} {
		out := mustDispatch(t, d, intent, map[string]string{"period": "month", "bucket": "day"}, dash.KindQuery)
		if out["usageEnabled"] != false {
			t.Fatal("disabled collection presented as measured")
		}
		if intent == "usage.summary" {
			for _, field := range []string{"totalRequests", "totalCostUsd", "totalTokens", "unpricedRequests", "avgLatencyMs", "cacheHitRate", "byOutcome"} {
				if out[field] != nil {
					t.Fatalf("%s unavailable value became zero", field)
				}
			}
		} else if len(out["items"].([]any)) != 0 {
			t.Fatal("disabled collection returned rows")
		}
	}
	s := gw.Store()
	one, two := storetest.InsertTenant(t, s), storetest.InsertTenant(t, s)
	k := storetest.Key(one.ID, "one")
	if err := s.Keys().Insert(context.Background(), k); err != nil {
		t.Fatal(err)
	}
	_, err := dispatch(t, d, "usage.records", fmt.Sprintf(`{"tenantId":%q,"keyId":%q}`, two.ID.String(), k.ID.String()), dash.KindQuery)
	if codeOf(err) != dash.CodeBadRequest {
		t.Fatal("key ownership mismatch accepted")
	}
}

func TestOverviewCountsAcrossPages(t *testing.T) {
	gw := testGateway(t, nexus.WithUsageEnabled(false))
	ctx := context.Background()
	for i := range 503 {
		_, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Tenant", Slug: fmt.Sprintf("tenant-%d", i)})
		if err != nil {
			t.Fatal(err)
		}
	}
	tn := storetest.InsertTenant(t, gw.Store())
	k := storetest.Key(tn.ID, "active")
	if err := gw.Store().Keys().Insert(ctx, k); err != nil {
		t.Fatal(err)
	}
	expired := storetest.Key(tn.ID, "expired")
	past := time.Now().Add(-time.Hour)
	expired.ExpiresAt = &past
	if err := gw.Store().Keys().Insert(ctx, expired); err != nil {
		t.Fatal(err)
	}
	if err := gw.Tenants().SetStatus(ctx, tn.ID.String(), tenant.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	out := mustDispatch(t, d, "overview.get", map[string]any{}, dash.KindQuery)
	counts := out["tenants"].(map[string]any)
	if counts["total"] != float64(504) || counts["active"] != float64(503) || counts["disabled"] != float64(1) || out["activeKeys"] != float64(1) {
		t.Fatal("overview used page length or stored key status as total")
	}
	if out["monthSpendUsd"] != nil || out["requestsToday"] != nil {
		t.Fatal("usage off reported measured totals")
	}
}
