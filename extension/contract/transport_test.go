package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	"github.com/xraph/forge/extensions/dashboard"
	dashauth "github.com/xraph/forge/extensions/dashboard/auth"
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"
	"github.com/xraph/forge/extensions/dashboard/contract/idempotency"
	"github.com/xraph/forge/extensions/dashboard/contract/transport"
	"github.com/xraph/forge/extensions/dashboard/security"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/store/storetest"
)

type wireRig struct {
	h     http.Handler
	store *idempotency.InMemoryStore
	gw    *nexus.Gateway
	csrf  string
}

func newWireRig(t *testing.T) wireRig {
	t.Helper()
	gw := testGateway(t)
	r, w := dash.NewRegistry(), dash.NewWardenRegistry()
	s := idempotency.NewInMemoryStore()
	d := dispatcher.NewWithOptions(nil, dispatcher.WithIdempotencyStore(dashboard.AdaptIdempotencyStore(s)))
	if err := Register(d, r, w, Deps{Gateway: func() *nexus.Gateway { return gw }}); err != nil {
		t.Fatal(err)
	}
	csrf := security.NewCSRFManager()
	return wireRig{h: transport.NewHandlerWithCSRF(r, w, d, nil, csrf), store: s, gw: gw, csrf: csrf.GenerateToken()}
}
func (r wireRig) call(intent, payload, token, idem string) *httptest.ResponseRecorder {
	body, _ := json.Marshal(dash.Request{Envelope: "v1", Kind: dash.KindCommand, Contributor: "nexus", Intent: intent, IntentVersion: 1, Payload: json.RawMessage(payload), CSRF: token, IdempotencyKey: idem})
	req := httptest.NewRequestWithContext(context.Background(), http.MethodPost, "/api/dashboard/v1", bytes.NewReader(body))
	req = req.WithContext(dashauth.WithUser(req.Context(), &dashauth.UserInfo{Subject: "operator"}))
	w := httptest.NewRecorder()
	r.h.ServeHTTP(w, req)
	return w
}
func wireKey(t *testing.T, w *httptest.ResponseRecorder, wantInvalidates []string) secretKeyResponse {
	t.Helper()
	if w.Code != http.StatusOK {
		t.Fatalf("command status %d", w.Code)
	}
	var envelope dash.Response
	if json.Unmarshal(w.Body.Bytes(), &envelope) != nil {
		t.Fatal("invalid envelope")
	}
	if !reflect.DeepEqual(envelope.Meta.Invalidates, wantInvalidates) {
		t.Fatal("incorrect wire invalidations")
	}
	var out secretKeyResponse
	if json.Unmarshal(envelope.Data, &out) != nil || !key.WellFormed(out.RawKey) {
		t.Fatal("command omitted one-time key")
	}
	return out
}
func TestSecretCommandsUseTombstonesOverForgeTransport(t *testing.T) {
	r := newWireRig(t)
	tn := storetest.InsertTenant(t, r.gw.Store())
	payload := fmt.Sprintf(`{"tenantId":%q,"name":"wire","scopes":["models"]}`, tn.ID.String())
	if w := r.call("keys.create", payload, "invalid", "create"); w.Code != http.StatusForbidden {
		t.Fatal("bad CSRF reached command")
	}
	created := wireKey(t, r.call("keys.create", payload, r.csrf, "create"), []string{"keys.list", "tenants.get", "overview.get"})
	rotated := wireKey(t, r.call("keys.rotate", fmt.Sprintf(`{"id":%q}`, created.Key.ID), r.csrf, "rotate"), []string{"keys.list", "keys.get", "tenants.get"})
	for _, tc := range []struct{ intent, payload, idem, raw string }{
		{"keys.create", payload, "create", created.RawKey},
		{"keys.rotate", fmt.Sprintf(`{"id":%q}`, created.Key.ID), "rotate", rotated.RawKey},
	} {
		entry, ok := r.store.Lookup(context.Background(), tc.idem, "operator:"+tc.intent)
		if !ok || len(entry.WireBody) != 0 || entry.Status != dispatcher.TombstoneStatus {
			t.Fatal("secret command retained a response body")
		}
		retry := r.call(tc.intent, tc.payload, r.csrf, tc.idem)
		var replay dash.ErrorResponse
		if json.Unmarshal(retry.Body.Bytes(), &replay) != nil || replay.Error == nil || replay.Error.Code != dash.CodeConflict || retry.Code != http.StatusInternalServerError || bytes.Contains(retry.Body.Bytes(), []byte(tc.raw)) {
			t.Fatal("secret was replayed or command reran")
		}
	}
	count, err := r.gw.Keys().Count(context.Background(), nil)
	if err != nil || count != 2 {
		t.Fatal("retry minted another key")
	}
}
func TestConcurrentKeyCreateMintsOnce(t *testing.T) {
	r := newWireRig(t)
	tn := storetest.InsertTenant(t, r.gw.Store())
	payload := fmt.Sprintf(`{"tenantId":%q,"name":"concurrent"}`, tn.ID.String())
	type result struct {
		status int
		code   dash.ErrorCode
	}
	results := make(chan result, 12)
	var wg sync.WaitGroup
	for range 12 {
		wg.Go(func() {
			w := r.call("keys.create", payload, r.csrf, "same")
			var envelope dash.ErrorResponse
			_ = json.Unmarshal(w.Body.Bytes(), &envelope)
			code := dash.ErrorCode("")
			if envelope.Error != nil {
				code = envelope.Error.Code
			}
			results <- result{status: w.Code, code: code}
		})
	}
	wg.Wait()
	close(results)
	successes := 0
	for got := range results {
		if got.status == http.StatusOK {
			successes++
		} else if got.status != http.StatusInternalServerError || got.code != dash.CodeConflict {
			t.Fatalf("unexpected response status %d code %s", got.status, got.code)
		}
	}
	count, err := r.gw.Keys().Count(context.Background(), nil)
	if err != nil || count != 1 || successes != 1 {
		t.Fatal("overlapping retries minted multiple keys")
	}
}

func TestTenantRenameInvalidatesKeyQueries(t *testing.T) {
	r := newWireRig(t)
	tn := storetest.InsertTenant(t, r.gw.Store())
	created := wireKey(t, r.call("keys.create", fmt.Sprintf(`{"tenantId":%q,"name":"rename"}`, tn.ID.String()), r.csrf, "create-for-rename"), []string{"keys.list", "tenants.get", "overview.get"})
	reply := r.call("tenants.update", fmt.Sprintf(`{"id":%q,"name":"Renamed"}`, tn.ID.String()), r.csrf, "rename")
	var out dash.Response
	if reply.Code != http.StatusOK || json.Unmarshal(reply.Body.Bytes(), &out) != nil {
		t.Fatal("tenant rename failed")
	}
	want := []string{"tenants.list", "tenants.get", "overview.get", "keys.list", "keys.get", "usage.records"}
	if !reflect.DeepEqual(out.Meta.Invalidates, want) {
		t.Fatal("tenant rename did not refresh dependent key labels")
	}
	row, err := keysGet(context.Background(), r.gw, idRequest{ID: created.Key.ID})
	if err != nil || row.TenantName != "Renamed" {
		t.Fatal("refreshed key did not carry the renamed tenant")
	}
}
