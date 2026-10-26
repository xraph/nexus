package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// toMock sends every request to target, keeping its path, so a provider
// configured for https://api.openai.com/v1 can be tested without leaving
// the machine.
type toMock struct {
	target *url.URL
	host   string // the host the request was built for
}

func (m *toMock) RoundTrip(r *http.Request) (*http.Response, error) {
	m.host = r.URL.Host
	r = r.Clone(r.Context())
	r.URL.Scheme, r.URL.Host = m.target.Scheme, m.target.Host
	return http.DefaultTransport.RoundTrip(r)
}

// officialAtMock is openai.New("k") with its requests rerouted to srv.
func officialAtMock(t *testing.T, srv *testutil.MockServer, opts ...Option) (*Provider, *toMock) {
	t.Helper()
	target, err := url.Parse(srv.Server.URL)
	if err != nil {
		t.Fatal(err)
	}
	rt := &toMock{target: target}
	p := New("k", opts...)
	p.WrapTransport(func(http.RoundTripper) http.RoundTripper { return rt })
	return p, rt
}

func lastBody(t *testing.T, srv *testutil.MockServer) map[string]any {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(srv.Ctrl.GetLastBody(), &body); err != nil {
		t.Fatalf("body: %v", err)
	}
	return body
}

func askFor500(ctx context.Context, p *Provider) error {
	_, err := p.Complete(ctx, &provider.CompletionRequest{
		Model: "gpt-4o", MaxTokens: 500,
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	return err
}

func wantMaxField(t *testing.T, body map[string]any, field string) {
	t.Helper()
	other := "max_tokens"
	if field == other {
		other = "max_completion_tokens"
	}
	if body[field] != float64(500) {
		t.Errorf("%s = %v, want 500", field, body[field])
	}
	if v, ok := body[other]; ok {
		t.Errorf("%s = %v sent next to %s", other, v, field)
	}
}

func TestOfficialEndpointSendsMaxCompletionTokens(t *testing.T) {
	srv := testutil.NewMockServer(t)
	p, rt := officialAtMock(t, srv)
	if err := askFor500(context.Background(), p); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if rt.host != "api.openai.com" || srv.Ctrl.GetLastPath() != "/v1/chat/completions" {
		t.Fatalf("request built for %s%s, want api.openai.com/v1/chat/completions", rt.host, srv.Ctrl.GetLastPath())
	}
	wantMaxField(t, lastBody(t, srv), "max_completion_tokens")

	// A stream follows the same rule.
	srv.Ctrl.SetStreamHandler(testutil.OpenAIStreamHandler([]string{testutil.OpenAIChunkJSON("id-1", "gpt-4o", "hi", "stop")}))
	s, err := p.CompleteStream(context.Background(), &provider.CompletionRequest{
		Model: "gpt-4o", MaxTokens: 500, Stream: true,
		Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	for {
		if _, err := s.Next(context.Background()); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Next: %v", err)
			}
			break
		}
	}
	_ = s.Close()
	wantMaxField(t, lastBody(t, srv), "max_completion_tokens")
}

func TestTenantCredentialOnOpenAIsOwnHostSendsMaxCompletionTokens(t *testing.T) {
	srv := testutil.NewMockServer(t)
	p, _ := officialAtMock(t, srv, WithBaseURL("http://localhost:1/v1"))
	ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "t", BaseURL: "https://api.openai.com/v1/"})
	if err := askFor500(ctx, p); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	wantMaxField(t, lastBody(t, srv), "max_completion_tokens")
}

func TestCompatibleEndpointKeepsMaxTokens(t *testing.T) {
	srv := testutil.NewMockServer(t)
	if err := askFor500(context.Background(), New("k", WithBaseURL(srv.Server.URL))); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	wantMaxField(t, lastBody(t, srv), "max_tokens")

	if err := askFor500(context.Background(), New("k", WithBaseURL(srv.Server.URL), WithMaxCompletionTokens(true))); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	wantMaxField(t, lastBody(t, srv), "max_completion_tokens")
}

func TestCacheTokensAreReported(t *testing.T) {
	srv := testutil.NewMockServer(t)
	srv.Ctrl.SetCompletion(map[string]any{
		"id": "c1", "object": "chat.completion", "created": 1, "model": "gpt-6.1-sol",
		"choices": []any{map[string]any{"index": 0, "message": map[string]any{"role": "assistant", "content": "hi"}, "finish_reason": "stop"}},
		"usage": map[string]any{
			"prompt_tokens": 12000, "completion_tokens": 10, "total_tokens": 12010,
			"prompt_tokens_details": map[string]any{"cached_tokens": 4000, "cache_write_tokens": 7000},
		},
	})
	resp, err := New("k", WithBaseURL(srv.Server.URL)).Complete(context.Background(), &provider.CompletionRequest{
		Model: "gpt-6.1-sol", Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if u := resp.Usage; u.PromptTokens != 12000 || u.CacheReadTokens != 4000 || u.CacheWriteTokens != 7000 {
		t.Fatalf("usage = %+v, want prompt 12000 with 4000 read and 7000 written", u)
	}

	srv.Ctrl.SetStreamHandler(testutil.OpenAIStreamHandler([]string{
		testutil.OpenAIChunkJSON("id-1", "gpt-6.1-sol", "hi", "stop"),
		`{"id":"id-1","object":"chat.completion.chunk","created":1,"model":"gpt-6.1-sol","choices":[],` +
			`"usage":{"prompt_tokens":3000,"completion_tokens":5,"total_tokens":3005,"prompt_tokens_details":{"cached_tokens":1024,"cache_write_tokens":512}}}`,
	}))
	s, err := New("k", WithBaseURL(srv.Server.URL)).CompleteStream(context.Background(), &provider.CompletionRequest{
		Model: "gpt-6.1-sol", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = s.Close() }()
	for {
		if _, err := s.Next(context.Background()); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("Next: %v", err)
			}
			break
		}
	}
	if u := s.Usage(); u == nil || u.CacheReadTokens != 1024 || u.CacheWriteTokens != 512 {
		t.Fatalf("stream usage = %+v, want 1024 read and 512 written", u)
	}
}
