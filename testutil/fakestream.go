package testutil

import (
	"context"
	"errors"
	"io"
	"sync"

	"github.com/xraph/nexus/provider"
)

// FakeStream is a deterministic provider.Stream backed by a slice of chunks.
// It returns each chunk in order, then io.EOF. Use BlockOn to inject a wait
// before delivery (e.g. to test heartbeat / cancellation behaviour).
type FakeStream struct {
	mu        sync.Mutex
	chunks    []*provider.StreamChunk
	idx       int
	closed    bool
	closedAt  int
	usage     *provider.Usage
	closeFunc func()
}

// NewFakeStream returns a FakeStream that emits the supplied chunks.
func NewFakeStream(chunks []*provider.StreamChunk, usage *provider.Usage) *FakeStream {
	return &FakeStream{chunks: chunks, usage: usage}
}

// SetCloseFunc registers a callback fired when Close is called. Useful for
// tests that need to assert clean shutdown.
func (f *FakeStream) SetCloseFunc(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closeFunc = fn
}

// Closed reports whether Close has been called.
func (f *FakeStream) Closed() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.closed
}

// ChunksRead returns the number of chunks that were actually delivered
// before the stream was closed or canceled.
func (f *FakeStream) ChunksRead() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.idx
}

// Next implements provider.Stream.
func (f *FakeStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil, errors.New("testutil: stream closed")
	}
	if f.idx >= len(f.chunks) {
		return nil, io.EOF
	}
	c := f.chunks[f.idx]
	f.idx++
	return c, nil
}

// Close implements provider.Stream.
func (f *FakeStream) Close() error {
	f.mu.Lock()
	if f.closed {
		f.mu.Unlock()
		return nil
	}
	f.closed = true
	f.closedAt = f.idx
	cb := f.closeFunc
	f.mu.Unlock()
	if cb != nil {
		cb()
	}
	return nil
}

// Usage implements provider.Stream.
func (f *FakeStream) Usage() *provider.Usage { return f.usage }
