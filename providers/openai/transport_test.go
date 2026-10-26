package openai

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// countingTransport counts the requests it carries and passes them on.
type countingTransport struct {
	next http.RoundTripper
	n    atomic.Int32
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return c.next.RoundTrip(r)
}

func TestWrapTransportCarriesEveryCall(t *testing.T) {
	srv := testutil.NewMockServer(t)
	p := New("sk-test", WithBaseURL(srv.Server.URL))

	ct := &countingTransport{next: http.DefaultTransport}
	p.WrapTransport(func(rt http.RoundTripper) http.RoundTripper {
		if tr, ok := rt.(*http.Transport); !ok || tr.ResponseHeaderTimeout != defaultTimeout {
			t.Errorf("the client starts on its own transport with the response-header timeout, got %T", rt)
		}
		return ct
	})

	if _, err := p.Complete(context.Background(), &provider.CompletionRequest{Model: "gpt-4o"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if n := ct.n.Load(); n != 1 {
		t.Fatalf("wrapped transport saw %d requests, want 1", n)
	}

	// A nil result leaves the transport alone.
	p.WrapTransport(func(http.RoundTripper) http.RoundTripper { return nil })
	if p.client.http.Transport != ct {
		t.Fatalf("a nil wrapper result replaced the transport with %T", p.client.http.Transport)
	}
}
