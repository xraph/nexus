package anthropic_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/anthropic"
	"github.com/xraph/nexus/testutil"
)

// anthropicCalls runs one request of each kind the provider sends.
var anthropicCalls = map[string]func(ctx context.Context, p *anthropic.Provider) error{
	"complete": func(ctx context.Context, p *anthropic.Provider) error {
		_, err := p.Complete(ctx, &provider.CompletionRequest{Model: "claude-opus-5-5", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
		return err
	},
	"stream": func(ctx context.Context, p *anthropic.Provider) error {
		s, err := p.CompleteStream(ctx, &provider.CompletionRequest{Model: "claude-opus-5-5", Stream: true, Messages: []provider.Message{{Role: "user", Content: "hi"}}})
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
}

func anthropicMock(t *testing.T) *testutil.MockServer {
	t.Helper()
	m := testutil.NewMockServer(t)
	m.Ctrl.SetCompletion(anthropicCompletionResp())
	m.Ctrl.SetStreamHandler(testutil.AnthropicStreamHandler([]string{
		testutil.AnthropicEventJSON("message_stop", map[string]any{"type": "message_stop"}),
		"",
	}))
	return m
}

func TestAnthropicUsesContextCredential(t *testing.T) {
	for name, call := range anthropicCalls {
		t.Run(name, func(t *testing.T) {
			mock := anthropicMock(t)
			p := anthropic.New("", anthropic.WithBaseURL(mock.Server.URL))

			ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "tenant"})
			if err := call(ctx, p); err != nil {
				t.Fatalf("call with a tenant key: %v", err)
			}
			if got := mock.Ctrl.GetLastHeader().Get("x-api-key"); got != "tenant" {
				t.Fatalf("x-api-key = %q, want tenant", got)
			}

			// Without one, an unconfigured provider fails as it always has.
			before := mock.Ctrl.GetLastBody()
			err := call(context.Background(), p)
			if !errors.Is(err, provider.ErrMissingAPIKey) {
				t.Fatalf("err = %v, want ErrMissingAPIKey", err)
			}
			if string(mock.Ctrl.GetLastBody()) != string(before) {
				t.Fatal("a request went out without a key")
			}
		})
	}
}

func TestAnthropicUsesContextBaseURL(t *testing.T) {
	for name, call := range anthropicCalls {
		t.Run(name, func(t *testing.T) {
			configured := anthropicMock(t)
			tenant := anthropicMock(t)
			p := anthropic.New("global", anthropic.WithBaseURL(configured.Server.URL))

			ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "tenant", BaseURL: tenant.Server.URL})
			if err := call(ctx, p); err != nil {
				t.Fatalf("call: %v", err)
			}
			if got := tenant.Ctrl.GetLastHeader().Get("x-api-key"); got != "tenant" || tenant.Ctrl.GetLastPath() != "/v1/messages" {
				t.Fatalf("tenant endpoint saw key %q path %q", got, tenant.Ctrl.GetLastPath())
			}
			if configured.Ctrl.GetLastPath() != "" {
				t.Fatal("the configured base URL was called")
			}

			if err := call(context.Background(), p); err != nil {
				t.Fatalf("call without credential: %v", err)
			}
			if got := configured.Ctrl.GetLastHeader().Get("x-api-key"); got != "global" {
				t.Fatalf("x-api-key = %q, want global", got)
			}
		})
	}
}

func TestAnthropicWithoutBaseURLFails(t *testing.T) {
	for name, call := range anthropicCalls {
		t.Run(name, func(t *testing.T) {
			p := anthropic.New("k", anthropic.WithBaseURL(""))
			if err := call(context.Background(), p); !errors.Is(err, provider.ErrMissingBaseURL) {
				t.Fatalf("err = %v, want ErrMissingBaseURL", err)
			}
		})
	}
}

func TestAnthropicAcceptsCredentials(t *testing.T) {
	var p provider.Provider = anthropic.New("")
	aware, ok := p.(provider.CredentialAware)
	if !ok || !aware.AcceptsCredentials() {
		t.Fatal("anthropic must accept tenant credentials")
	}
}

func TestAnthropicErrorsNeverCarryTheKey(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = fmt.Fprintf(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key %s"}}`, r.Header.Get("x-api-key"))
	}))
	t.Cleanup(srv.Close)

	for name, call := range anthropicCalls {
		t.Run(name, func(t *testing.T) {
			p := anthropic.New("global", anthropic.WithBaseURL(srv.URL))
			ctx := provider.WithCredential(context.Background(), provider.Credential{APIKey: "sk-ant-tenant-secret"})
			err := call(ctx, p)
			if err == nil {
				t.Fatal("want an error from the 401")
			}
			if strings.Contains(err.Error(), "sk-ant-tenant-secret") {
				t.Fatalf("error carries the tenant key: %v", err)
			}
			if !strings.Contains(err.Error(), "status 401") {
				t.Fatalf("err = %v, want the status kept", err)
			}
		})
	}
}
