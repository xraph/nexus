package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/loader"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/store/storetest"
)

func TestManifestMatchesAllEighteenIntents(t *testing.T) {
	m, err := loader.Load(bytes.NewReader(manifestYAML), "manifest.yaml")
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string][]string{
		"overview.get": nil, "tenants.list": nil, "tenants.get": nil, "keys.list": nil, "keys.get": nil,
		"usage.summary": nil, "usage.series": nil, "usage.records": nil, "models.list": nil, "providers.list": nil, "gateway.get": nil, "settings.get": nil,
		"tenants.create":    {"tenants.list", "overview.get"},
		"tenants.update":    {"tenants.list", "tenants.get", "overview.get", "keys.list", "keys.get", "usage.records"},
		"tenants.setStatus": {"tenants.list", "tenants.get", "overview.get"},
		"keys.create":       {"keys.list", "tenants.get", "overview.get"},
		"keys.rotate":       {"keys.list", "keys.get", "tenants.get"},
		"keys.revoke":       {"keys.list", "keys.get", "tenants.get", "overview.get"},
	}
	if len(m.Intents) != 18 {
		t.Fatal("incomplete contract")
	}
	for _, in := range m.Intents {
		invalidates, ok := expected[in.Name]
		if !ok || in.Version != 1 || !reflect.DeepEqual(in.Invalidates, invalidates) {
			t.Fatalf("incorrect intent declaration: %s", in.Name)
		}
		kind := dash.IntentKindQuery
		if invalidates != nil {
			kind = dash.IntentKindCommand
		}
		if in.Kind != kind {
			t.Fatal("incorrect intent kind")
		}
		delete(expected, in.Name)
	}
	if len(expected) != 0 {
		t.Fatal("missing intent")
	}
}
func TestEveryReadExcludesCreatedAndRotatedSecrets(t *testing.T) {
	gw := testGateway(t)
	tn := storetest.InsertTenant(t, gw.Store())
	other := storetest.InsertTenant(t, gw.Store())
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	created := mustDispatch(t, d, "keys.create", map[string]string{"tenantId": tn.ID.String(), "name": "One time"}, dash.KindCommand)
	originalID := created["key"].(map[string]any)["id"].(string)
	rotated := mustDispatch(t, d, "keys.rotate", map[string]string{"id": originalID}, dash.KindCommand)
	replacementID := rotated["key"].(map[string]any)["id"].(string)
	secrets := make([]string, 0, 4)
	secrets = append(secrets, created["rawKey"].(string), rotated["rawKey"].(string))
	for _, kid := range []string{originalID, replacementID} {
		k, err := gw.Store().Keys().FindByID(context.Background(), kid)
		if err != nil {
			t.Fatal("stored key unavailable")
		}
		secrets = append(secrets, k.Hash)
	}
	queries := map[string]map[string]any{
		"overview.get": {}, "tenants.list": {}, "tenants.get": {"id": tn.ID.String()}, "keys.list": {}, "keys.get": {"id": replacementID},
		"usage.summary": {"period": "month"}, "usage.series": {"period": "month", "bucket": "day"}, "usage.records": {}, "models.list": {}, "providers.list": {}, "gateway.get": {}, "settings.get": {},
	}
	// Params are the React client's actual query envelope. Principal claims
	// cannot silently narrow the operator-wide query to a different tenant.
	principal := dash.PrincipalFor(&dashauth.UserInfo{Subject: "operator", Claims: map[string]any{"tenantId": other.ID.String()}})
	for intent, params := range queries {
		b, _, err := d.Dispatch(context.Background(), dash.Request{Envelope: "v1", Kind: dash.KindQuery, Contributor: "nexus", Intent: intent, IntentVersion: 1, Params: params}, principal)
		if err != nil {
			t.Fatalf("read failed: %s", intent)
		}
		for _, secret := range secrets {
			if secret == "" || bytes.Contains(b, []byte(secret)) {
				t.Fatal("read exposed key material")
			}
		}
		if intent == "tenants.list" {
			var out tenantsListResponse
			if json.Unmarshal(b, &out) != nil || len(out.Items) != 2 {
				t.Fatal("principal claims changed operator scope")
			}
		}
	}
	for _, intent := range []string{"keys.list", "usage.summary", "usage.series", "usage.records"} {
		_, _, err := d.Dispatch(context.Background(), dash.Request{Envelope: "v1", Kind: dash.KindQuery, Contributor: "nexus", Intent: intent, IntentVersion: 1, Params: map[string]any{"tenantId": nil, "period": "month", "bucket": "day"}}, principal)
		if codeOf(err) != dash.CodeBadRequest {
			t.Fatal("null query params widened tenant scope")
		}
	}
	_, err := dispatch(t, d, "keys.get", fmt.Sprintf(`{"id":%q}`, originalID), dash.KindQuery)
	if err != nil {
		t.Fatal("revoked key detail unavailable")
	}
}
