package guard

import (
	"context"
	"errors"
	"io"

	"github.com/xraph/nexus/provider"
)

// StreamGuard applies content safety checks to streaming responses.
type StreamGuard interface {
	Guard

	// CheckChunk evaluates a single stream chunk.
	CheckChunk(ctx context.Context, chunk *provider.StreamChunk) (*CheckResult, error)
}

// StreamStrategy controls how guards are applied to streams.
type StreamStrategy string

const (
	// StrategyBuffer buffers the entire response, then checks it.
	StrategyBuffer StreamStrategy = "buffer"

	// StrategyPassthrough lets chunks through immediately with post-hoc audit.
	StrategyPassthrough StreamStrategy = "passthrough"

	// StrategyChunkwise checks each chunk individually as it arrives.
	StrategyChunkwise StreamStrategy = "chunkwise"
)

// GuardedStream wraps a provider.Stream and applies guardrails.
type GuardedStream struct {
	inner    provider.Stream
	guards   []StreamGuard
	strategy StreamStrategy

	// For buffer strategy
	chunks   []*provider.StreamChunk
	buffered bool
	idx      int
}

// NewGuardedStream creates a new guarded stream wrapper.
func NewGuardedStream(inner provider.Stream, guards []StreamGuard, strategy StreamStrategy) *GuardedStream {
	return &GuardedStream{
		inner:    inner,
		guards:   guards,
		strategy: strategy,
	}
}

// Next returns the next chunk, applying the configured guard strategy.
func (gs *GuardedStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	switch gs.strategy {
	case StrategyBuffer:
		return gs.nextBuffered(ctx)
	case StrategyChunkwise:
		return gs.nextChunkwise(ctx)
	default: // passthrough
		return gs.inner.Next(ctx)
	}
}

// Close releases resources.
func (gs *GuardedStream) Close() error {
	return gs.inner.Close()
}

// Usage returns final usage after stream completes.
func (gs *GuardedStream) Usage() *provider.Usage {
	return gs.inner.Usage()
}

// nextBuffered buffers all chunks, checks them, then replays.
func (gs *GuardedStream) nextBuffered(ctx context.Context) (*provider.StreamChunk, error) {
	if !gs.buffered {
		// Buffer all chunks
		for {
			chunk, err := gs.inner.Next(ctx)
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			gs.chunks = append(gs.chunks, chunk)
		}

		// Run guards on buffered content
		for _, g := range gs.guards {
			for _, chunk := range gs.chunks {
				result, err := g.CheckChunk(ctx, chunk)
				if err != nil {
					return nil, err
				}
				if result.Blocked {
					return nil, &BlockedError{Guard: g.Name(), Reason: result.Reason}
				}
			}
		}
		gs.buffered = true
	}

	// Replay buffered chunks
	if gs.idx >= len(gs.chunks) {
		return nil, io.EOF
	}
	chunk := gs.chunks[gs.idx]
	gs.idx++
	return chunk, nil
}

// nextChunkwise checks each chunk as it arrives.
func (gs *GuardedStream) nextChunkwise(ctx context.Context) (*provider.StreamChunk, error) {
	chunk, err := gs.inner.Next(ctx)
	if err != nil {
		return nil, err
	}

	for _, g := range gs.guards {
		result, err := g.CheckChunk(ctx, chunk)
		if err != nil {
			return nil, err
		}
		if result.Blocked {
			_ = gs.inner.Close()
			return nil, &BlockedError{Guard: g.Name(), Reason: result.Reason}
		}
	}

	return chunk, nil
}

// BlockedError is returned when a stream guard blocks content.
type BlockedError struct {
	Guard  string
	Reason string
}

func (e *BlockedError) Error() string {
	return "nexus: stream blocked by guard " + e.Guard + ": " + e.Reason
}
