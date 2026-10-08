package contract

import (
	"bytes"
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
)

func TestKeyLifecycleKeepsSecretsOutOfReads(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		tn := storetest.InsertTenant(t, s)
		gw := testGateway(t, nexus.WithDatabase(s))
		d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
		created := mustDispatch(t, d, "keys.create", map[string]any{"tenantId": tn.ID.String(), "name": "Runner", "scopes": []string{"models"}}, dash.KindCommand)
		raw, ok := created["rawKey"].(string)
		if !ok || !key.WellFormed(raw) {
			t.Fatal("create did not return a key")
		}
		krow := created["key"].(map[string]any)
		kid := krow["id"].(string)
		stored, err := s.Keys().FindByID(ctx, kid)
		if err != nil {
			t.Fatalf("read newly created key: %v", err)
		}
		if stored.Hash == "" || stored.Hash == raw {
			t.Fatal("key was not hashed")
		}
		if _, validateErr := gw.Keys().Validate(ctx, raw); validateErr != nil {
			t.Fatal("created key is unusable")
		}
		// Validate touches lastUsedAt. Both creation and this clock must be readable.
		if _, readErr := gw.Keys().Get(ctx, kid); readErr != nil {
			t.Fatalf("read used key: %v", readErr)
		}
		for _, in := range []struct{ intent, payload string }{{"keys.get", fmt.Sprintf(`{"id":%q}`, kid)}, {"keys.list", `{}`}, {"tenants.get", fmt.Sprintf(`{"id":%q}`, tn.ID.String())}, {"overview.get", `{}`}, {"settings.get", `{}`}, {"gateway.get", `{}`}} {
			b, readErr := dispatch(t, d, in.intent, in.payload, dash.KindQuery)
			if readErr != nil {
				t.Fatal("read failed")
			}
			if bytes.Contains(b, []byte(raw)) || bytes.Contains(b, []byte(stored.Hash)) {
				t.Fatal("read exposed key material")
			}
		}
		rotated := mustDispatch(t, d, "keys.rotate", map[string]string{"id": kid}, dash.KindCommand)
		replacement := rotated["key"].(map[string]any)["id"].(string)
		nextRaw := rotated["rawKey"].(string)
		if replacement == kid || rotated["revokedKeyId"] != kid || nextRaw == raw {
			t.Fatal("rotation did not replace key")
		}
		old, err := gw.Keys().Get(ctx, kid)
		if err != nil || old.Status != key.KeyRevoked {
			t.Fatal("rotation left old key usable")
		}
		if _, err := gw.Keys().Validate(ctx, nextRaw); err != nil {
			t.Fatal("replacement key is unusable")
		}
		revoked := mustDispatch(t, d, "keys.revoke", map[string]string{"id": replacement}, dash.KindCommand)
		if revoked["status"] != "revoked" {
			t.Fatal("revoke did not report committed state")
		}
		if _, err := gw.Keys().Validate(ctx, nextRaw); err == nil {
			t.Fatal("revoked key still authenticates")
		}
	})
}
func TestKeyPagingAndEffectiveStatus(t *testing.T) {
	storetest.Each(t, func(t *testing.T, s store.Store) {
		ctx := context.Background()
		one, two := storetest.InsertTenant(t, s), storetest.InsertTenant(t, s)
		a, b, c := storetest.Key(one.ID, "active"), storetest.Key(one.ID, "expired"), storetest.Key(two.ID, "revoked")
		past := time.Now().Add(-time.Hour)
		b.ExpiresAt = &past
		c.Status = key.KeyRevoked
		for _, k := range []*key.APIKey{a, b, c} {
			if err := s.Keys().Insert(ctx, k); err != nil {
				t.Fatal(err)
			}
		}
		gw := testGateway(t, nexus.WithDatabase(s))
		d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
		for _, tc := range []struct{ status, kid string }{{"active", a.ID.String()}, {"expired", b.ID.String()}, {"revoked", c.ID.String()}} {
			out := mustDispatch(t, d, "keys.list", map[string]string{"status": tc.status}, dash.KindQuery)
			items := out["items"].([]any)
			if len(items) != 1 || items[0].(map[string]any)["id"] != tc.kid || items[0].(map[string]any)["status"] != tc.status {
				t.Fatal("status filter disagrees with effective expiry")
			}
		}
		cursor := ""
		seen := map[string]bool{}
		for {
			out := mustDispatch(t, d, "keys.list", map[string]any{"tenantId": one.ID.String(), "limit": 1, "cursor": cursor}, dash.KindQuery)
			for _, raw := range out["items"].([]any) {
				row := raw.(map[string]any)
				kid := row["id"].(string)
				if seen[kid] || row["tenantId"] != one.ID.String() || row["tenantName"] != one.Name {
					t.Fatal("key paging lost identity")
				}
				seen[kid] = true
			}
			cursor = out["nextCursor"].(string)
			if cursor == "" {
				break
			}
		}
		if len(seen) != 2 {
			t.Fatal("key paging missed a row")
		}
	})
}
func TestKeyContractValidation(t *testing.T) {
	gw := testGateway(t)
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return gw }})
	tn := storetest.InsertTenant(t, gw.Store())
	for _, raw := range []string{`null`, `""`, `" "`, `4`, `"invalid"`} {
		_, err := dispatch(t, d, "keys.list", `{"tenantId":`+raw+`}`, dash.KindQuery)
		if codeOf(err) != dash.CodeBadRequest {
			t.Fatal("unusable key scope widened query")
		}
	}
	for _, suffix := range []string{`,"scopes":[]`, `,"scopes":null`, `,"scopes":["unknown"]`, `,"expiresAt":"bad"`, `,"expiresAt":"2000-01-01T00:00:00Z"`} {
		_, err := dispatch(t, d, "keys.create", fmt.Sprintf(`{"tenantId":%q,"name":"test"%s}`, tn.ID.String(), suffix), dash.KindCommand)
		if codeOf(err) != dash.CodeBadRequest {
			t.Fatal("invalid create accepted")
		}
	}
	for _, intent := range []string{"keys.get", "keys.rotate", "keys.revoke"} {
		kind := dash.KindCommand
		if intent == "keys.get" {
			kind = dash.KindQuery
		}
		_, err := dispatch(t, d, intent, `{"id":"bad"}`, kind)
		if codeOf(err) != dash.CodeBadRequest {
			t.Fatal("malformed key ID accepted")
		}
		_, err = dispatch(t, d, intent, fmt.Sprintf(`{"id":%q}`, id.NewKeyID().String()), kind)
		if codeOf(err) != dash.CodeNotFound {
			t.Fatal("missing key did not return not found")
		}
	}
	_, err := dispatch(t, d, "keys.list", fmt.Sprintf(`{"tenantId":%q}`, id.NewTenantID().String()), dash.KindQuery)
	if codeOf(err) != dash.CodeNotFound {
		t.Fatal("unknown tenant widened query")
	}
}
