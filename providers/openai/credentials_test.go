package openai

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// callKind runs one request of each kind the client sends.
var callKinds = map[string]func(ctx context.Context, p *Provider) error{
	"complete": func(ctx context.Context, p *Provider) error {
		_, err := p.Complete(ctx, &provider.CompletionRequest{Model: "gpt-4o", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
		return err
	},
	"stream": func(ctx context.Context, p *Provider) error {
		s, err := p.CompleteStream(ctx, &provider.CompletionRequest{Model: "gpt-4o", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}})
		if err != nil {
			return err
		}
		defer func() { _ = s.Close() }()
		for {
			if _, err := s.Next(ctx); err != nil {
				if errors.Is(err, io.EOF) {
					return nil
				}
				return err
			}
		}
	},
	"embed": func(ctx context.Context, p *Provider) error {
		_, err := p.Embed(ctx, &provider.EmbeddingRequest{Model: "text-embedding-3-small", Input: []string{"hi"}})
		return err
	},
}

func streamingMock(t *testing.T) *testutil.MockServer {
	t.Helper()
	m := testutil.NewMockServer(t)
	m.Ctrl.SetStreamHandler(func(w http.ResponseWriter, _ *http.Request) {
		testutil.WriteSSEChunks(w, []string{testutil.OpenAIChunkJSON("id-1", "gpt-4o", "hi", "stop")})
	})
	return m
}

func TestOpenAIUsesContextCredential(t *testing.T) {
	for name, call := range callKinds {
		t.Run(name, func(t *testing.T) {
			configured := streamingMock(t)
			tenant := streamingMock(t)
			p := New("global", WithBaseURL(configured.Server.URL))

			ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "tenant", BaseURL: tenant.Server.URL + "/"})
			if err := call(ctx, p); err != nil {
				t.Fatalf("call: %v", err)
			}
			if got := tenant.Ctrl.GetLastHeader().Get("Authorization"); got != "Bearer tenant" {
				t.Fatalf("Authorization = %q, want Bearer tenant", got)
			}
			if path := tenant.Ctrl.GetLastPath(); path != "/chat/completions" && path != "/embeddings" {
				t.Fatalf("path = %q; a trailing slash on the tenant base URL must not double up", path)
			}
			if configured.Ctrl.GetLastPath() != "" {
				t.Fatal("the configured base URL was called")
			}

			// The next request without a credential goes back to the
			// configured key and URL.
			if err := call(context.Background(), p); err != nil {
				t.Fatalf("call without credential: %v", err)
			}
			if got := configured.Ctrl.GetLastHeader().Get("Authorization"); got != "Bearer global" {
				t.Fatalf("Authorization = %q, want Bearer global", got)
			}
		})
	}
}

func TestOpenAIContextKeyKeepsTheConfiguredBaseURL(t *testing.T) {
	m := streamingMock(t)
	p := New("global", WithBaseURL(m.Server.URL))
	ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "tenant"})
	if err := callKinds["complete"](ctx, p); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := m.Ctrl.GetLastHeader().Get("Authorization"); got != "Bearer tenant" {
		t.Fatalf("Authorization = %q, want Bearer tenant", got)
	}
}

func TestOpenAIWithoutBaseURLFails(t *testing.T) {
	var transport countingTransport
	transport.next = http.DefaultTransport
	for name, call := range callKinds {
		t.Run(name, func(t *testing.T) {
			p := New("k", WithBaseURL(""))
			p.WrapTransport(func(http.RoundTripper) http.RoundTripper { return &transport })
			err := call(context.Background(), p)
			if !errors.Is(err, provider.ErrMissingBaseURL) {
				t.Fatalf("err = %v, want ErrMissingBaseURL", err)
			}
		})
	}
	if n := transport.n.Load(); n != 0 {
		t.Fatalf("%d requests went out; the missing base URL must fail before any I/O", n)
	}
}

func TestOpenAIErrorsNeverCarryTheKey(t *testing.T) {
	// A server that echoes the key it was sent, as some compatible servers
	// do in their 401 bodies.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"error":{"message":"invalid key %s"}}`, strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
	}))
	t.Cleanup(srv.Close)

	for name, call := range callKinds {
		t.Run(name, func(t *testing.T) {
			p := New("global", WithBaseURL(srv.URL))
			ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "sk-tenant-secret"})
			err := call(ctx, p)
			if err == nil {
				t.Fatal("want an error from the 401")
			}
			if strings.Contains(err.Error(), "sk-tenant-secret") {
				t.Fatalf("error carries the tenant key: %v", err)
			}
			if !strings.Contains(err.Error(), "status 401") {
				t.Fatalf("err = %v, want the status kept", err)
			}
		})
	}
}

func TestStreamOutlivesTheResponseHeaderTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		for i := range 5 {
			time.Sleep(40 * time.Millisecond)
			_, _ = fmt.Fprintf(w, "data: %s\n\n", testutil.OpenAIChunkJSON("id-1", "gpt-4o", fmt.Sprintf("chunk%d", i), ""))
			flusher.Flush()
		}
		_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
		flusher.Flush()
	}))
	t.Cleanup(srv.Close)

	p := New("k", WithBaseURL(srv.URL), WithTimeout(50*time.Millisecond))
	ctx := context.Background()
	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{Model: "gpt-4o", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	var got []string
	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Next after %d chunks: %v", len(got), err)
		}
		if chunk.Delta.Content != "" {
			got = append(got, chunk.Delta.Content)
		}
	}
	if len(got) != 5 {
		t.Fatalf("got %d chunks %q, want 5", len(got), got)
	}
}

func TestCompleteHonoursTheResponseHeaderTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-release:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(testutil.DefaultCompletionResponse())
	}))
	t.Cleanup(srv.Close)
	t.Cleanup(func() { close(release) })

	p := New("k", WithBaseURL(srv.URL), WithTimeout(50*time.Millisecond))
	start := time.Now()
	err := callKinds["complete"](context.Background(), p)
	if err == nil {
		t.Fatal("Complete succeeded, want a response-header timeout")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Errorf("err = %v, want it to mention a timeout", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Complete returned after %v, want within 1s", elapsed)
	}
}

func TestOpenAITenantKeyDropsTheOperatorsOrganization(t *testing.T) {
	m := streamingMock(t)
	p := New("global", WithBaseURL(m.Server.URL), WithOrgID("org-operator"))

	ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "tenant"})
	if err := callKinds["complete"](ctx, p); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := m.Ctrl.GetLastHeader().Get("OpenAI-Organization"); got != "" {
		t.Fatalf("OpenAI-Organization = %q sent with a tenant key", got)
	}

	if err := callKinds["complete"](context.Background(), p); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if got := m.Ctrl.GetLastHeader().Get("OpenAI-Organization"); got != "org-operator" {
		t.Fatalf("OpenAI-Organization = %q, want org-operator with the configured key", got)
	}
}

func TestOpenAIBaseURLCredentialNeverGetsTheConfiguredKey(t *testing.T) {
	configured := streamingMock(t)
	chosen := streamingMock(t)
	p := New("global", WithBaseURL(configured.Server.URL), WithOrgID("org-operator"))

	ctx := provider.WithCredential(context.Background(), provider.Credential{BaseURL: chosen.Server.URL})
	if err := callKinds["complete"](ctx, p); err != nil {
		t.Fatalf("complete: %v", err)
	}
	h := chosen.Ctrl.GetLastHeader()
	if h.Get("Authorization") != "" || h.Get("OpenAI-Organization") != "" {
		t.Fatalf("the chosen host got the operator's credentials: Authorization %q, org %q", h.Get("Authorization"), h.Get("OpenAI-Organization"))
	}
	if configured.Ctrl.GetLastPath() != "" {
		t.Fatal("the configured base URL was called")
	}
}

func TestOpenAIShortKeysAreNotCutFromErrors(t *testing.T) {
	m := testutil.NewMockServer(t)
	m.Ctrl.SetStatusCode(http.StatusUnauthorized)
	p := New("k", WithBaseURL(m.Server.URL))
	err := callKinds["complete"](context.Background(), p)
	if err == nil || !strings.Contains(err.Error(), "mock error") {
		t.Fatalf("err = %v, want the body intact (a one-letter key must not be cut out of it)", err)
	}
}

// otherRoundTripper stands in for an http.DefaultTransport someone replaced.
type otherRoundTripper struct{}

func (otherRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unused")
}

func TestReplacedDefaultTransportKeepsTheStockSettings(t *testing.T) {
	tr := newTransport(otherRoundTripper{}, time.Minute)
	if tr.DialContext == nil || tr.TLSHandshakeTimeout == 0 || tr.IdleConnTimeout == 0 || !tr.ForceAttemptHTTP2 || tr.Proxy == nil {
		t.Fatalf("fallback transport lost the stock settings: %+v", tr)
	}
	if tr.ResponseHeaderTimeout != time.Minute {
		t.Fatalf("ResponseHeaderTimeout = %v, want 1m", tr.ResponseHeaderTimeout)
	}
}
