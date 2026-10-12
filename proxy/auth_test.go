package proxy_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/proxy"
	"github.com/xraph/nexus/store"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// okProvider answers every completion with 10 prompt and 5 completion
// tokens, and lists one priced model.
type okProvider struct{}

func (okProvider) Name() string { return "openai" }
func (okProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Chat: true, Streaming: true, Embeddings: true}
}
func (okProvider) Models(context.Context) ([]provider.Model, error) {
	return []provider.Model{{ID: "gpt-4o", Name: "gpt-4o", Provider: "openai", Pricing: provider.Pricing{InputPerMillion: money.MustParse("2.50"), OutputPerMillion: money.MustParse("10")}}}, nil
}
func (okProvider) Complete(_ context.Context, req *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return &provider.CompletionResponse{Provider: "openai", Model: req.Model,
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "hi"}}},
		Usage:   provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15}}, nil
}
func (okProvider) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return nil, errors.New("not used")
}
func (okProvider) Embed(_ context.Context, req *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return &provider.EmbeddingResponse{Provider: "openai", Model: req.Model, Usage: provider.Usage{PromptTokens: 5, TotalTokens: 5}}, nil
}
func (okProvider) Healthy(context.Context) bool { return true }

func newProxy(t *testing.T, opts ...nexus.Option) (*httptest.Server, *nexus.Gateway, string) {
	t.Helper()
	s := store.NewMemory()
	gw := nexus.New(append([]nexus.Option{nexus.WithDatabase(s), nexus.WithProvider(okProvider{})}, opts...)...)
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	tn, err := gw.Tenants().Create(context.Background(), &tenant.CreateInput{Name: "A", Slug: "a", Quota: &tenant.Quota{RPM: 1}})
	if err != nil {
		t.Fatal(err)
	}
	_, user, err := gw.Keys().Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: "u"})
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(proxy.New(gw.Engine()))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	return srv, gw, user
}

func newKey(t *testing.T, gw *nexus.Gateway, slug string, q tenant.Quota, scopes ...string) (string, *tenant.Tenant) {
	t.Helper()
	tn, err := gw.Tenants().Create(context.Background(), &tenant.CreateInput{Name: slug, Slug: slug, Quota: &q})
	if err != nil {
		t.Fatal(err)
	}
	_, raw, err := gw.Keys().Create(context.Background(), &key.CreateInput{TenantID: tn.ID.String(), Name: slug, Scopes: scopes})
	if err != nil {
		t.Fatal(err)
	}
	return raw, tn
}

const chatBody = `{"model":"gpt-4o","messages":[{"role":"user","content":"hi"}]}`

type reply struct {
	status int
	header http.Header
	body   string
	// shown is body with the request key redacted: failure messages print it.
	shown string
	// shape is the OpenAI error envelope, when the body is one.
	message, typ, code string
	hasError           bool
}

// send makes a request. rawKey "" sends no key. A failure message names the
// path and status only: it never carries the key.
// redact hides the key wherever a failure message would print a body.
func redact(body, rawKey string) string {
	if rawKey == "" {
		return body
	}
	return strings.ReplaceAll(body, rawKey, "<key>")
}

func send(t *testing.T, srv *httptest.Server, method, path, rawKey, body string) reply {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), method, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if rawKey != "" {
		req.Header.Set("Authorization", "Bearer "+rawKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	b, _ := io.ReadAll(resp.Body)
	r := reply{status: resp.StatusCode, header: resp.Header, body: string(b), shown: redact(string(b), rawKey)}
	var env struct {
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(b, &env) == nil && env.Error != nil {
		r.hasError, r.message, r.typ, r.code = true, env.Error.Message, env.Error.Type, env.Error.Code
	}
	return r
}

// wantRefusal asserts the status and the OpenAI error shape, with a code.
func wantRefusal(t *testing.T, got reply, status int, code string) {
	t.Helper()
	if got.status != status || got.code != code {
		t.Fatalf("status %d code %q, want %d %q; body %s", got.status, got.code, status, code, got.shown)
	}
	if !got.hasError || got.message == "" || got.typ == "" {
		t.Fatalf("body is not {\"error\":{message,type,code}}: %s", got.shown)
	}
}

func wantNoKeyIn(t *testing.T, got reply, rawKey string) {
	t.Helper()
	if strings.Contains(got.body, rawKey) {
		t.Fatalf("the response body echoes the key (status %d)", got.status)
	}
	for k, vs := range got.header {
		for _, v := range vs {
			if strings.Contains(v, rawKey) {
				t.Fatalf("the %s header echoes the key", k)
			}
		}
	}
}

func records(t *testing.T, gw *nexus.Gateway, opts *usage.QueryOptions) []*usage.Record {
	t.Helper()
	if err := gw.FlushUsage(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := gw.Usage().Query(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	return res.Items
}

func TestHealthNeedsNoKey(t *testing.T) {
	srv, _, _ := newProxy(t)
	if got := send(t, srv, "GET", "/health", "", ""); got.status != 200 {
		t.Fatalf("status %d, want 200", got.status)
	}
}

func TestAMissingKeyIsRefusedAndNotRecorded(t *testing.T) {
	srv, gw, _ := newProxy(t)
	got := send(t, srv, "POST", "/v1/chat/completions", "", chatBody)
	wantRefusal(t, got, 401, "unauthenticated")
	if got.typ != "authentication_error" {
		t.Fatalf("type %q, want authentication_error", got.typ)
	}
	if got.header.Get("WWW-Authenticate") == "" {
		t.Fatal("a 401 must say how to authenticate")
	}
	if n := len(records(t, gw, &usage.QueryOptions{})); n != 0 {
		t.Fatalf("%d usage records, want none: a refusal at the edge has no tenant to charge", n)
	}
}

func TestAnUnknownKeyIsRefusedWithoutEchoingIt(t *testing.T) {
	srv, _, _ := newProxy(t)
	bad := "nxs_" + strings.Repeat("ab", 32)
	got := send(t, srv, "POST", "/v1/chat/completions", bad, chatBody)
	wantRefusal(t, got, 401, "unauthenticated")
	wantNoKeyIn(t, got, bad)
}

func TestARevokedKeyIsRefusedAtTheEdge(t *testing.T) {
	srv, gw, user := newProxy(t)
	k, err := gw.Keys().Validate(context.Background(), user)
	if err != nil {
		t.Fatal(err)
	}
	if err := gw.Keys().Revoke(context.Background(), k.ID.String()); err != nil {
		t.Fatal(err)
	}
	got := send(t, srv, "POST", "/v1/chat/completions", user, chatBody)
	wantRefusal(t, got, 401, "unauthenticated")
	wantNoKeyIn(t, got, user)
}

func TestModelsNeedTheModelsScope(t *testing.T) {
	srv, gw, user := newProxy(t)
	if got := send(t, srv, "GET", "/v1/models", user, ""); got.status != 200 {
		t.Fatalf("default key: status %d, want 200; body %s", got.status, got.shown)
	}
	if got := send(t, srv, "GET", "/v1/models/gpt-4o", user, ""); got.status != 200 {
		t.Fatalf("default key, one model: status %d, want 200; body %s", got.status, got.shown)
	}
	only, _ := newKey(t, gw, "only-completions", tenant.Quota{}, "completions")
	wantRefusal(t, send(t, srv, "GET", "/v1/models", only, ""), 403, "forbidden")
	wantRefusal(t, send(t, srv, "GET", "/v1/models/gpt-4o", only, ""), 403, "forbidden")
	wantRefusal(t, send(t, srv, "GET", "/v1/models", "", ""), 401, "unauthenticated")
}

func TestAnOpenGatewayListsModelsWithoutAKey(t *testing.T) {
	srv, _, _ := newProxy(t, nexus.WithRequireAPIKey(false))
	for _, path := range []string{"/v1/models", "/v1/models/gpt-4o"} {
		if got := send(t, srv, "GET", path, "", ""); got.status != 200 {
			t.Fatalf("%s: status %d, want 200; body %s", path, got.status, got.shown)
		}
	}
}

func TestAnOpenGatewayStillChecksAKeyThatIsPresented(t *testing.T) {
	srv, _, _ := newProxy(t, nexus.WithRequireAPIKey(false))
	bad := "nxs_" + strings.Repeat("cd", 32)
	got := send(t, srv, "POST", "/v1/chat/completions", bad, chatBody)
	wantRefusal(t, got, 401, "unauthenticated")
	wantNoKeyIn(t, got, bad)
}

func TestRateLimitedIs429WithRetryAfter(t *testing.T) {
	srv, _, user := newProxy(t)
	if got := send(t, srv, "POST", "/v1/chat/completions", user, chatBody); got.status != 200 {
		t.Fatalf("first: status %d, want 200; body %s", got.status, got.shown)
	}
	got := send(t, srv, "POST", "/v1/chat/completions", user, chatBody)
	wantRefusal(t, got, 429, "rate_limited")
	secs, err := strconv.Atoi(got.header.Get("Retry-After"))
	if err != nil || secs < 1 || secs > 60 {
		t.Fatalf("Retry-After %q, want whole seconds between 1 and 60", got.header.Get("Retry-After"))
	}
}

func TestAKeyWithoutCompletionsIsRefusedAndRecorded(t *testing.T) {
	srv, gw, _ := newProxy(t)
	emb, tn := newKey(t, gw, "embeddings-only", tenant.Quota{}, "embeddings")
	wantRefusal(t, send(t, srv, "POST", "/v1/chat/completions", emb, chatBody), 403, "forbidden")
	recs := records(t, gw, &usage.QueryOptions{TenantID: tn.ID.String()})
	if len(recs) != 1 || recs[0].Outcome != usage.OutcomeRefused || recs[0].RefusalCode != "forbidden" {
		t.Fatalf("records %d, want one refused/forbidden", len(recs))
	}
}

func TestAStreamRefusalIsAStatusNotAStream(t *testing.T) {
	srv, gw, _ := newProxy(t)
	emb, _ := newKey(t, gw, "embeddings-only", tenant.Quota{}, "embeddings")
	got := send(t, srv, "POST", "/v1/chat/completions", emb, `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	wantRefusal(t, got, 403, "forbidden")
	if ct := got.header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type %q, want JSON: the refusal came before any SSE", ct)
	}
}

func TestEmbeddingsAreRefusedWithTheirOwnStatus(t *testing.T) {
	srv, gw, user := newProxy(t)
	if got := send(t, srv, "POST", "/v1/embeddings", user, `{"model":"gpt-4o","input":"x"}`); got.status != 200 {
		t.Fatalf("status %d, want 200; body %s", got.status, got.shown)
	}
	comp, _ := newKey(t, gw, "completions-only", tenant.Quota{}, "completions")
	wantRefusal(t, send(t, srv, "POST", "/v1/embeddings", comp, `{"model":"gpt-4o","input":"x"}`), 403, "forbidden")
	wantRefusal(t, send(t, srv, "POST", "/v1/embeddings", "", `{"model":"gpt-4o","input":"x"}`), 401, "unauthenticated")
}

func TestAnOpenGatewayServesWithoutAKey(t *testing.T) {
	srv, _, _ := newProxy(t, nexus.WithRequireAPIKey(false))
	if got := send(t, srv, "POST", "/v1/chat/completions", "", chatBody); got.status != 200 {
		t.Fatalf("status %d, want 200; body %s", got.status, got.shown)
	}
}

// failingSpend is a usage service whose MonthlySpend fails.
type failingSpend struct{ usage.Service }

func (failingSpend) MonthlySpend(context.Context, string) (money.USD, error) {
	return money.USD{}, errors.New("db down at postgres://u:secret@db/x")
}

func TestEveryPipelineRefusalMapsToItsStatus(t *testing.T) {
	ctx := context.Background()
	s := store.NewMemory()
	srvDown, gwDown, _ := newProxy(t, nexus.WithDatabase(s), nexus.WithUsageService(failingSpend{usage.NewService(s.Usage())}))
	srvOK, gwOK, _ := newProxy(t)

	rows := []struct {
		name   string
		srv    *httptest.Server
		gw     *nexus.Gateway
		quota  tenant.Quota
		body   string
		warm   bool // send one request and flush usage first
		setup  func(t *testing.T, gw *nexus.Gateway, tn *tenant.Tenant)
		status int
		code   string
		retry  bool
	}{
		{name: "max_tokens above the cap", srv: srvOK, gw: gwOK, quota: tenant.Quota{MaxTokensPerReq: 100},
			body: `{"model":"gpt-4o","max_tokens":500,"messages":[{"role":"user","content":"hi"}]}`, status: 400, code: "invalid_request"},
		{name: "daily requests used up", srv: srvOK, gw: gwOK, quota: tenant.Quota{DailyRequests: 1}, body: chatBody, warm: true,
			status: 429, code: "quota_exceeded", retry: true},
		{name: "budget crossed", srv: srvOK, gw: gwOK, quota: tenant.Quota{MonthlyBudgetUSD: money.MustParse("0.00005")}, body: chatBody, warm: true,
			status: 429, code: "budget_exceeded", retry: true},
		{name: "suspended tenant", srv: srvOK, gw: gwOK, quota: tenant.Quota{}, body: chatBody,
			setup: func(t *testing.T, gw *nexus.Gateway, tn *tenant.Tenant) {
				if err := gw.Tenants().SetStatus(ctx, tn.ID.String(), tenant.StatusSuspended); err != nil {
					t.Fatal(err)
				}
			}, status: 403, code: "forbidden"},
		{name: "usage service down", srv: srvDown, gw: gwDown, quota: tenant.Quota{MonthlyBudgetUSD: money.MustParse("10")}, body: chatBody,
			status: 503, code: "unavailable"},
	}
	for i, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			raw, tn := newKey(t, row.gw, "row-"+strconv.Itoa(i), row.quota)
			if row.setup != nil {
				row.setup(t, row.gw, tn)
			}
			if row.warm {
				if got := send(t, row.srv, "POST", "/v1/chat/completions", raw, row.body); got.status != 200 {
					t.Fatalf("first request: status %d, want 200; body %s", got.status, got.shown)
				}
				if err := row.gw.FlushUsage(ctx); err != nil {
					t.Fatal(err)
				}
			}
			got := send(t, row.srv, "POST", "/v1/chat/completions", raw, row.body)
			wantRefusal(t, got, row.status, row.code)
			if row.retry {
				if secs, err := strconv.Atoi(got.header.Get("Retry-After")); err != nil || secs < 1 {
					t.Fatalf("Retry-After %q, want whole seconds of at least 1", got.header.Get("Retry-After"))
				}
			}
			if strings.Contains(got.body, "postgres://") || strings.Contains(got.body, "secret") {
				t.Fatalf("the body leaks the cause: %s", got.shown)
			}
		})
	}
}

type leakyProvider struct{ okProvider }

func (leakyProvider) Complete(context.Context, *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("POST https://api.example.com/v1?key=secret: boom")
}

func TestAnInternalErrorIsAFixed500AndItsCauseIsLogged(t *testing.T) {
	srv, _, logs, raw, _ := serve(t, leakyProvider{})
	got := send(t, srv, "POST", "/v1/chat/completions", raw, chatBody)
	wantRefusal(t, got, 500, "internal_error")
	if strings.Contains(got.body, "secret") || strings.Contains(got.body, "https://") {
		t.Fatalf("the body leaks the provider error: %s", got.shown)
	}
	if out := logs.text(); !strings.Contains(out, "boom") || strings.Contains(out, raw) {
		t.Fatalf("the log must name the cause and never the key; logged: %s", redact(out, raw))
	}
}

func TestAPreflightNeedsNoKey(t *testing.T) {
	srv, _, _ := newProxy(t)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodOptions, srv.URL+"/v1/chat/completions", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(resp.Header.Get("Access-Control-Allow-Headers"), "x-api-key") {
		t.Fatalf("Allow-Headers %q must list x-api-key, which a browser client sends the key in", resp.Header.Get("Access-Control-Allow-Headers"))
	}
}

// recordingLogger keeps what the gateway logs.
type recordingLogger struct {
	mu    sync.Mutex
	lines []string
}

func (l *recordingLogger) add(msg string, args []any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	parts := make([]string, 1, 1+len(args))
	parts[0] = msg
	for _, a := range args {
		parts = append(parts, fmt.Sprint(a))
	}
	l.lines = append(l.lines, strings.Join(parts, " "))
}
func (l *recordingLogger) text() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.lines, "\n")
}
func (l *recordingLogger) Debug(msg string, args ...any) { l.add(msg, args) }
func (l *recordingLogger) Info(msg string, args ...any)  { l.add(msg, args) }
func (l *recordingLogger) Warn(msg string, args ...any)  { l.add(msg, args) }
func (l *recordingLogger) Error(msg string, args ...any) { l.add(msg, args) }

// boomProvider streams one chunk, then fails with a message that carries a
// URL and a secret.
type boomProvider struct{ okProvider }

func (boomProvider) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return &boomStream{}, nil
}

type boomStream struct{ sent bool }

func (s *boomStream) Next(context.Context) (*provider.StreamChunk, error) {
	if !s.sent {
		s.sent = true
		return &provider.StreamChunk{Provider: "openai", Model: "gpt-4o", Delta: provider.Delta{Content: "he"}}, nil
	}
	return nil, errors.New("POST https://api.example.com/v1?key=secret: boom")
}
func (*boomStream) Close() error           { return nil }
func (*boomStream) Usage() *provider.Usage { return nil }

// serve builds a gateway over p with a recording logger, and a key for a
// tenant with no limits.
func serve(t *testing.T, p provider.Provider) (*httptest.Server, *nexus.Gateway, *recordingLogger, string, *tenant.Tenant) {
	t.Helper()
	logs := &recordingLogger{}
	gw := nexus.New(nexus.WithDatabase(store.NewMemory()), nexus.WithProvider(p), nexus.WithLogger(logs))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = gw.Shutdown(context.Background()) })
	srv := httptest.NewServer(proxy.New(gw.Engine()))
	t.Cleanup(srv.Close)
	raw, tn := newKey(t, gw, "serve", tenant.Quota{})
	return srv, gw, logs, raw, tn
}

func TestAStreamThatFailsMidwayIsLoggedAndTheClientSeesAFixedMessage(t *testing.T) {
	srv, _, logs, raw, _ := serve(t, boomProvider{})
	got := send(t, srv, "POST", "/v1/chat/completions", raw, `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`)
	if got.status != 200 {
		t.Fatalf("status %d, want 200 (the stream had begun); body %s", got.status, got.shown)
	}
	if !strings.Contains(got.body, "upstream error") {
		t.Fatalf("the error event must carry the fixed message; body %s", got.shown)
	}
	if strings.Contains(got.body, "secret") || strings.Contains(got.body, "https://") {
		t.Fatalf("the stream leaks the provider error: %s", got.shown)
	}
	out := logs.text()
	if !strings.Contains(out, "boom") || strings.Contains(out, raw) {
		t.Fatalf("the log must name the cause and never the key; logged: %s", redact(out, raw))
	}
	if rid := got.header.Get("X-Request-Id"); rid == "" || !strings.Contains(out, rid) {
		t.Fatalf("the log line must carry the request id %q; logged: %s", rid, redact(out, raw))
	}
}

func TestTheRequestIdIsTheSameInTheHeaderTheLogAndTheUsageRecord(t *testing.T) {
	srv, gw, logs, raw, tn := serve(t, leakyProvider{})
	// A client-supplied id is ignored: the gateway sets its own.
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1/chat/completions", strings.NewReader(chatBody))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+raw)
	req.Header.Set("X-Request-Id", "req_client_supplied")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	rid := resp.Header.Get("X-Request-Id")
	if resp.StatusCode != 500 || rid == "" || rid == "req_client_supplied" {
		t.Fatalf("status %d, X-Request-Id %q: want a 500 with an id the gateway made", resp.StatusCode, rid)
	}
	if out := logs.text(); !strings.Contains(out, rid) || !strings.Contains(out, "boom") {
		t.Fatalf("the 5xx log line must carry the id %q and the cause; logged: %s", rid, redact(out, raw))
	}
	recs := records(t, gw, &usage.QueryOptions{TenantID: tn.ID.String()})
	if len(recs) != 1 || recs[0].RequestID.String() != rid {
		t.Fatalf("%d usage records; want one whose RequestID is the header's %q", len(recs), rid)
	}
}

func TestARefusalAtTheEdgeCarriesARequestId(t *testing.T) {
	srv, _ := mustServe(t)
	got := send(t, srv, "POST", "/v1/chat/completions", "", chatBody)
	wantRefusal(t, got, 401, "unauthenticated")
	if got.header.Get("X-Request-Id") == "" {
		t.Fatal("a refusal at the edge must carry X-Request-Id")
	}
}

func TestRealtimeNeedsAKeyBeforeTheUpgrade(t *testing.T) {
	srv, _ := mustServe(t)
	wantRefusal(t, send(t, srv, "GET", "/v1/realtime", "", ""), 401, "unauthenticated")
	bad := "nxs_" + strings.Repeat("ef", 32)
	got := send(t, srv, "GET", "/v1/realtime", bad, "")
	wantRefusal(t, got, 401, "unauthenticated")
	wantNoKeyIn(t, got, bad)
}

func mustServe(t *testing.T) (*httptest.Server, *nexus.Gateway) {
	t.Helper()
	srv, gw, _ := newProxy(t)
	return srv, gw
}

func TestAKeyOfADisabledTenantIsRefusedAtTheEdge(t *testing.T) {
	srv, gw, _ := newProxy(t)
	raw, tn := newKey(t, gw, "off", tenant.Quota{})
	if err := gw.Tenants().SetStatus(context.Background(), tn.ID.String(), tenant.StatusDisabled); err != nil {
		t.Fatal(err)
	}
	got := send(t, srv, "POST", "/v1/chat/completions", raw, chatBody)
	wantRefusal(t, got, 403, "forbidden")
	wantNoKeyIn(t, got, raw)
	// The pipeline records the refusals it makes. None here: the edge
	// refused it before the pipeline ran.
	if n := len(records(t, gw, &usage.QueryOptions{})); n != 0 {
		t.Fatalf("%d usage records; want none, the edge refuses before the pipeline", n)
	}
}

func TestTheRequestIdIsReadableFromABrowser(t *testing.T) {
	srv, _, _ := newProxy(t)
	got := send(t, srv, "GET", "/health", "", "")
	if !strings.Contains(got.header.Get("Access-Control-Expose-Headers"), "X-Request-Id") {
		t.Fatalf("Access-Control-Expose-Headers = %q; a browser page cannot read X-Request-Id", got.header.Get("Access-Control-Expose-Headers"))
	}
}

// withDefaultModel sets the tenant's default model.
func withDefaultModel(t *testing.T, gw *nexus.Gateway, tn *tenant.Tenant, model string) {
	t.Helper()
	if _, err := gw.Tenants().Update(context.Background(), tn.ID.String(), &tenant.UpdateInput{Config: &tenant.Config{DefaultModel: model}}); err != nil {
		t.Fatal(err)
	}
}

func TestARequestWithNoModelGetsTheTenantsDefault(t *testing.T) {
	srv, gw, _ := newProxy(t)
	raw, tn := newKey(t, gw, "defaulted", tenant.Quota{})
	withDefaultModel(t, gw, tn, "gpt-4o")

	got := send(t, srv, "POST", "/v1/chat/completions", raw, `{"messages":[{"role":"user","content":"hi"}]}`)
	if got.status != 200 || !strings.Contains(got.body, `"model":"gpt-4o"`) {
		t.Fatalf("completion: status %d, body %s; want 200 served by the default model", got.status, got.shown)
	}
	got = send(t, srv, "POST", "/v1/embeddings", raw, `{"input":"x"}`)
	if got.status != 200 || !strings.Contains(got.body, `"model":"gpt-4o"`) {
		t.Fatalf("embedding: status %d, body %s; want 200 served by the default model", got.status, got.shown)
	}
}

func TestARequestWithNoModelAndNoDefaultIsRefusedAndRecorded(t *testing.T) {
	srv, gw, _ := newProxy(t)
	raw, tn := newKey(t, gw, "undefaulted", tenant.Quota{})

	wantRefusal(t, send(t, srv, "POST", "/v1/chat/completions", raw, `{"messages":[{"role":"user","content":"hi"}]}`), 400, "invalid_request")
	wantRefusal(t, send(t, srv, "POST", "/v1/embeddings", raw, `{"input":"x"}`), 400, "invalid_request")
	recs := records(t, gw, &usage.QueryOptions{TenantID: tn.ID.String()})
	if len(recs) != 2 {
		t.Fatalf("records = %d, want 2", len(recs))
	}
	for _, r := range recs {
		if r.Outcome != usage.OutcomeRefused || r.RefusalCode != "invalid_request" {
			t.Fatalf("record = outcome %s code %q; want refused/invalid_request", r.Outcome, r.RefusalCode)
		}
	}
}
