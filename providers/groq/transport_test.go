package groq

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

type countingTransport struct{ n atomic.Int32 }

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	c.n.Add(1)
	return http.DefaultTransport.RoundTrip(r)
}

func TestWrapTransportReachesTheOpenAIClient(t *testing.T) {
	srv := testutil.NewMockServer(t)
	p := New("test-key", WithBaseURL(srv.Server.URL))
	ct := &countingTransport{}
	p.WrapTransport(func(http.RoundTripper) http.RoundTripper { return ct })

	if _, err := p.Complete(context.Background(), &provider.CompletionRequest{Model: "llama-3.3-70b-versatile"}); err != nil {
		t.Fatalf("complete: %v", err)
	}
	if ct.n.Load() != 1 {
		t.Fatalf("wrapped transport saw %d requests, want 1", ct.n.Load())
	}
}
