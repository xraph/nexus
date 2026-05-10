package proxy_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/proxy"
)

// blockingProvider returns a stream that blocks Next forever until ctx
// cancels. Used to verify Shutdown actually tears in-flight streams.
type blockingProvider struct {
	closed atomic.Int32
	wg     sync.WaitGroup
}

func (p *blockingProvider) Name() string { return "blocking" }
func (p *blockingProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{Streaming: true}
}
func (p *blockingProvider) Models(_ context.Context) ([]provider.Model, error) { return nil, nil }
func (p *blockingProvider) Complete(_ context.Context, _ *provider.CompletionRequest) (*provider.CompletionResponse, error) {
	return nil, errors.New("not used")
}
func (p *blockingProvider) Embed(_ context.Context, _ *provider.EmbeddingRequest) (*provider.EmbeddingResponse, error) {
	return nil, errors.New("not used")
}
func (p *blockingProvider) Healthy(_ context.Context) bool { return true }

func (p *blockingProvider) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	p.wg.Add(1)
	return &blockingStream{owner: p}, nil
}

type blockingStream struct {
	owner *blockingProvider
}

func (s *blockingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("stream timed out")
	}
}

func (s *blockingStream) Close() error {
	s.owner.closed.Add(1)
	s.owner.wg.Done()
	return nil
}

func (s *blockingStream) Usage() *provider.Usage { return nil }

// TestProxy_ShutdownCancelsInFlightStreams: a stream is opened, then
// Shutdown is called. The stream's Close must be invoked promptly via the
// proxy's base-context cancellation chain.
func TestProxy_ShutdownCancelsInFlightStreams(t *testing.T) {
	t.Parallel()

	bp := &blockingProvider{}
	engine := nexus.NewEngine(nexus.WithProvider(bp))
	p := proxy.New(engine, proxy.WithoutWebSocket())

	srv := httptest.NewServer(p)
	t.Cleanup(srv.Close)

	// Fire a streaming request in the background and don't wait for it to
	// finish — we want it open so Shutdown has work to do.
	clientErrCh := make(chan error, 1)
	go func() {
		body := strings.NewReader(`{"model":"x","messages":[{"role":"user","content":"hi"}],"stream":true}`)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1/chat/completions", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			clientErrCh <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		// Drain — the stream will produce no chunks but the runner should
		// emit a [DONE] (or error event) once it observes ctx cancel.
		_, _ = io.ReadAll(resp.Body)
		clientErrCh <- nil
	}()

	// Give the request a moment to land at the server.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && bp.closed.Load() == 0 {
		// Wait until the provider has at least started returning a stream.
		// Detected indirectly via wg.
		time.Sleep(20 * time.Millisecond)
	}

	if err := p.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	// Wait for the proxy to tear the stream and the client to drain.
	select {
	case err := <-clientErrCh:
		_ = err // err can be non-nil (forced close); we only care that it returned
	case <-time.After(3 * time.Second):
		t.Fatal("client never returned after Shutdown")
	}
	if bp.closed.Load() == 0 {
		t.Fatal("provider stream's Close was never called after Shutdown")
	}
}
