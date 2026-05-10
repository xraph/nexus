package api_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	nexusapi "github.com/xraph/nexus/api"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
)

// blockingProvider blocks Next forever until ctx cancels.
type blockingProvider struct {
	closed atomic.Int32
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
	return &blockingStream{owner: p}, nil
}

type blockingStream struct{ owner *blockingProvider }

func (s *blockingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		return nil, errors.New("timeout")
	}
}

func (s *blockingStream) Close() error {
	s.owner.closed.Add(1)
	return nil
}

func (s *blockingStream) Usage() *provider.Usage { return nil }

func TestAPI_ShutdownCancelsInFlightStreams(t *testing.T) {
	t.Parallel()

	bp := &blockingProvider{}
	gw := nexus.New(nexus.WithProvider(bp))
	if err := gw.Initialize(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}

	a := nexusapi.New(gw, nexusapi.WithoutWebSocket())
	srv := httptest.NewServer(a.Handler())
	t.Cleanup(srv.Close)

	clientErr := make(chan error, 1)
	go func() {
		body := strings.NewReader(`{"model":"x","messages":[{"role":"user","content":"hi"}],"stream":true}`)
		req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/v1/chat/completions", body)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			clientErr <- err
			return
		}
		defer func() { _ = resp.Body.Close() }()
		_, _ = io.ReadAll(resp.Body)
		clientErr <- nil
	}()

	// Wait for the request to land.
	time.Sleep(150 * time.Millisecond)

	if err := a.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	select {
	case <-clientErr:
	case <-time.After(3 * time.Second):
		t.Fatal("client never returned after Shutdown")
	}
	if bp.closed.Load() == 0 {
		t.Fatal("upstream stream's Close was never invoked")
	}
}
