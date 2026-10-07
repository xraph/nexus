package gemini

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/xraph/nexus/provider"
)

// The API key goes in the x-goog-api-key header and never in the URL. A
// transport failure's *url.Error prints the whole URL, and that text ends up
// in the gateway log.
func TestTheKeyTravelsInAHeaderNeverInTheURL(t *testing.T) {
	const secret = "AIza-test-secret"
	type seen struct {
		path, query string
		header      bool
	}
	var (
		mu   sync.Mutex
		reqs []seen
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, seen{path: r.URL.Path, query: r.URL.RawQuery, header: r.Header.Get("x-goog-api-key") == secret})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()

	p := New(secret, WithBaseURL(srv.URL))
	ctx := context.Background()
	_, _ = p.Complete(ctx, &provider.CompletionRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}})
	if st, err := p.CompleteStream(ctx, &provider.CompletionRequest{Model: "m", Messages: []provider.Message{{Role: "user", Content: "hi"}}}); err == nil {
		_ = st.Close()
	}
	_, _ = p.Embed(ctx, &provider.EmbeddingRequest{Model: "e", Input: []string{"hi"}})
	_ = p.Healthy(ctx)

	mu.Lock()
	defer mu.Unlock()
	if len(reqs) != 4 {
		t.Fatalf("saw %d requests, want 4 (complete, stream, embed, health)", len(reqs))
	}
	for _, r := range reqs {
		if strings.Contains(r.query, "key=") || strings.Contains(r.query, secret) {
			t.Errorf("%s: the query carries the key", r.path)
		}
		if !r.header {
			t.Errorf("%s: no x-goog-api-key header with the key", r.path)
		}
	}
}
