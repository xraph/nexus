package contract

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

func codeOf(err error) dash.ErrorCode {
	var ce *dash.Error
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

func testGateway(t *testing.T, opts ...nexus.Option) *nexus.Gateway {
	t.Helper()
	gw := nexus.New(opts...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := gw.Shutdown(context.Background()); err != nil {
			t.Error(err)
		}
	})
	return gw
}

func testDispatcher(t *testing.T, deps Deps) *dispatcher.Dispatcher {
	t.Helper()
	d := dispatcher.New(nil)
	if err := Register(d, dash.NewRegistry(), dash.NewWardenRegistry(), deps); err != nil {
		t.Fatal(err)
	}
	return d
}

func dispatch(t *testing.T, d *dispatcher.Dispatcher, intent, payload string, kind dash.Kind) (json.RawMessage, error) {
	t.Helper()
	b, _, err := d.Dispatch(context.Background(), dash.Request{
		Envelope: "v1", Contributor: "nexus", Intent: intent, IntentVersion: 1,
		Kind: kind, Payload: json.RawMessage(payload),
	}, dash.Principal{})
	return b, err
}

func TestSettingsResolveGatewayAtRequestTime(t *testing.T) {
	var live *nexus.Gateway
	d := testDispatcher(t, Deps{Gateway: func() *nexus.Gateway { return live }})
	_, err := dispatch(t, d, "settings.get", `{}`, dash.KindQuery)
	var ce *dash.Error
	if !errors.As(err, &ce) || ce.Code != dash.CodeUnavailable || !ce.Retryable {
		t.Fatal("startup must be retryable unavailable")
	}
	live = testGateway(t, nexus.WithTimeout(2345*time.Millisecond), nexus.WithRequireAPIKey(false), nexus.WithUsageEnabled(false))
	b, err := dispatch(t, d, "settings.get", `{}`, dash.KindQuery)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatal(err)
	}
	if got["defaultTimeoutMs"] != float64(2345) || got["requireApiKey"] != false || got["usageEnabled"] != false {
		t.Fatal("settings did not reflect effective config")
	}
	if strings.Contains(strings.ToLower(string(b)), "bootstrap") {
		t.Fatal("settings exposed bootstrap configuration")
	}
}

func TestScopeDistinguishesOmittedFromUnusable(t *testing.T) {
	gw := testGateway(t)
	tn, err := gw.Tenants().Create(context.Background(), &tenant.CreateInput{Name: "One", Slug: "one"})
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		raw  string
		code dash.ErrorCode
		want string
	}{
		{"", "", ""}, {`null`, dash.CodeBadRequest, ""}, {`""`, dash.CodeBadRequest, ""},
		{`" "`, dash.CodeBadRequest, ""}, {`123`, dash.CodeBadRequest, ""}, {`{}`, dash.CodeBadRequest, ""},
		{`"bad"`, dash.CodeBadRequest, ""}, {`"` + id.NewTenantID().String() + `"`, dash.CodeNotFound, ""},
		{`"` + tn.ID.String() + `"`, "", tn.ID.String()},
	}
	for _, tc := range cases {
		got, err := tenantScope(context.Background(), gw, json.RawMessage(tc.raw))
		if codeOf(err) != tc.code || got != tc.want {
			t.Errorf("scope %q: got %q, code %s; want %q, code %s", tc.raw, got, codeOf(err), tc.want, tc.code)
		}
	}
}

func TestErrorsDoNotExposeInternalDetails(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code dash.ErrorCode
	}{
		{tenant.ErrNotFound, dash.CodeNotFound}, {key.ErrNotFound, dash.CodeNotFound},
		{tenant.ErrInvalid, dash.CodeBadRequest}, {key.ErrInvalid, dash.CodeBadRequest},
		{paging.ErrInvalidCursor, dash.CodeBadRequest}, {usage.ErrInvalidPeriod, dash.CodeBadRequest},
		{usage.ErrInvalidSeries, dash.CodeBadRequest}, {tenant.ErrInUse, dash.CodeConflict},
		{context.Canceled, dash.CodeUnavailable}, {context.DeadlineExceeded, dash.CodeUnavailable},
		{errors.New("private datastore details"), dash.CodeInternal},
	} {
		mapped := mapError(tc.err)
		if codeOf(mapped) != tc.code {
			t.Errorf("mapped %T to %s, want %s", tc.err, codeOf(mapped), tc.code)
		}
		if strings.Contains(mapped.Error(), "private datastore details") {
			t.Fatal("error exposed backend detail")
		}
	}
}

func TestRegisterRequiresAResolver(t *testing.T) {
	if err := Register(dispatcher.New(nil), dash.NewRegistry(), dash.NewWardenRegistry(), Deps{}); err == nil {
		t.Fatal("missing resolver accepted")
	}
}
