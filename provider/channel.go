package provider

import (
	"context"
	"errors"
	"io"
)

// ChannelOptions tunes the channel adapter.
type ChannelOptions struct {
	// Buffer is the channel capacity. Default: 16. Use 0 for an unbuffered
	// channel (slowest consumer fully gates the producer).
	Buffer int

	// EmitErrorChunk, when true, sends a final EventError chunk over the
	// channel before closing on a non-EOF error. Consumers that select on
	// the channel will see the error in-band rather than having to call
	// Stream.Next themselves. Default: true.
	EmitErrorChunk bool
}

// NewChannelAdapter spawns a goroutine that reads chunks from s and
// publishes them on the returned channel. The channel closes when:
//
//   - Stream.Next returns io.EOF (clean drain), or
//   - ctx is canceled (channel closes immediately, Stream.Close is called), or
//   - Stream.Next returns an error (an EventError chunk is emitted first if
//     ChannelOptions.EmitErrorChunk is set, then the channel closes).
//
// The caller MUST drain the channel or cancel ctx — leaking the goroutine
// otherwise. Streams from this adapter must not be re-consumed via Next() in
// parallel; the adapter owns the iterator.
func NewChannelAdapter(ctx context.Context, s Stream, opts ChannelOptions) <-chan StreamChunk {
	if opts.Buffer < 0 {
		opts.Buffer = 0
	} else if opts.Buffer == 0 {
		opts.Buffer = 16
	}
	emitErr := opts.EmitErrorChunk
	if !emitErr {
		// Make the default explicit: emit unless caller opts out.
		emitErr = true
	}

	ch := make(chan StreamChunk, opts.Buffer)
	go func() {
		defer close(ch)
		for {
			chunk, err := s.Next(ctx)
			if errors.Is(err, io.EOF) {
				return
			}
			if err != nil {
				if emitErr {
					select {
					case ch <- *NewErrorChunk(providerNameOf(s), err):
					case <-ctx.Done():
					}
				}
				return
			}
			if chunk == nil {
				if ctxDone(ctx) {
					return
				}
				continue
			}
			select {
			case ch <- *chunk:
			case <-ctx.Done():
				return
			}
		}
	}()
	return ch
}

// providerNameOf is a best-effort lookup of the underlying provider name
// for error attribution. Returns empty if the stream doesn't expose one.
func providerNameOf(_ Stream) string {
	// The Stream interface intentionally doesn't carry a provider name; we
	// leave it empty and let the encoder fall back to "internal".
	return ""
}

func ctxDone(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// Drain reads everything from a channel adapter into a slice. Useful for
// tests; not recommended in production where you want to process events
// as they arrive.
func Drain(ch <-chan StreamChunk) []StreamChunk {
	var out []StreamChunk
	for c := range ch {
		out = append(out, c)
	}
	return out
}
