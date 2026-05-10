package provider_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

func TestChannelAdapter_DrainsCleanlyOnEOF(t *testing.T) {
	t.Parallel()
	chunks := []*provider.StreamChunk{
		{Delta: provider.Delta{Content: "a"}},
		{Delta: provider.Delta{Content: "b"}, FinishReason: "stop"},
	}
	stream := testutil.NewFakeStream(chunks, nil)

	ch := provider.NewChannelAdapter(context.Background(), stream, provider.ChannelOptions{Buffer: 4})
	got := provider.Drain(ch)
	if len(got) != 2 {
		t.Fatalf("got %d events, want 2", len(got))
	}
	if got[0].Delta.Content != "a" || got[1].Delta.Content != "b" {
		t.Fatalf("unexpected events: %+v", got)
	}
}

func TestChannelAdapter_EmitsErrorChunkOnFailure(t *testing.T) {
	t.Parallel()
	stream := &errStream{err: errors.New("upstream broke")}

	ch := provider.NewChannelAdapter(context.Background(), stream, provider.ChannelOptions{})
	events := provider.Drain(ch)
	if len(events) != 1 {
		t.Fatalf("expected 1 error event, got %d", len(events))
	}
	if events[0].Kind != provider.EventError {
		t.Fatalf("expected EventError, got %s", events[0].Kind)
	}
	if events[0].Err != "upstream broke" {
		t.Fatalf("unexpected error message: %q", events[0].Err)
	}
}

func TestChannelAdapter_ContextCancelClosesPromptly(t *testing.T) {
	t.Parallel()
	stream := &blockingStream{}
	ctx, cancel := context.WithCancel(context.Background())

	ch := provider.NewChannelAdapter(ctx, stream, provider.ChannelOptions{Buffer: 1})
	cancel()

	select {
	case _, ok := <-ch:
		if ok {
			// drain remaining items so the goroutine exits cleanly
			for range ch {
				_ = struct{}{}
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("channel did not close after ctx cancel")
	}
}

type errStream struct{ err error }

func (s *errStream) Next(_ context.Context) (*provider.StreamChunk, error) { return nil, s.err }
func (s *errStream) Close() error                                          { return nil }
func (s *errStream) Usage() *provider.Usage                                { return nil }

type blockingStream struct{}

func (s *blockingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (s *blockingStream) Close() error           { return nil }
func (s *blockingStream) Usage() *provider.Usage { return nil }
