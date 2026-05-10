package provider_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/xraph/nexus/provider"
)

type fakeBiStream struct {
	mu     sync.Mutex
	sent   []provider.ClientEvent
	chunks []*provider.StreamChunk
	idx    int
	closed bool
}

func (f *fakeBiStream) Next(_ context.Context) (*provider.StreamChunk, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idx >= len(f.chunks) {
		return nil, errors.New("EOF-ish")
	}
	c := f.chunks[f.idx]
	f.idx++
	return c, nil
}

func (f *fakeBiStream) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed = true
	return nil
}

func (f *fakeBiStream) Usage() *provider.Usage { return nil }

func (f *fakeBiStream) Send(_ context.Context, evt provider.ClientEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, evt)
	return nil
}

func TestBiStream_AssertsAsBiStream(t *testing.T) {
	t.Parallel()
	var s provider.Stream = &fakeBiStream{}
	bi, ok := s.(provider.BiStream)
	if !ok {
		t.Fatal("fakeBiStream should satisfy BiStream")
	}
	if err := bi.Send(context.Background(), provider.ClientEvent{Type: "audio_chunk"}); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestClientEvent_AudioFields(t *testing.T) {
	t.Parallel()
	bi := &fakeBiStream{}
	if err := bi.Send(context.Background(), provider.ClientEvent{
		Type: "audio_chunk",
		Audio: &provider.AudioChunk{
			Format:     "pcm16",
			SampleRate: 24000,
			Data:       []byte{1, 2, 3, 4},
		},
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(bi.sent) != 1 || bi.sent[0].Audio == nil {
		t.Fatalf("sent: %+v", bi.sent)
	}
	if bi.sent[0].Audio.Format != "pcm16" || len(bi.sent[0].Audio.Data) != 4 {
		t.Fatalf("audio metadata lost: %+v", bi.sent[0].Audio)
	}
}
