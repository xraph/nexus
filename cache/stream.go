package cache

import (
	"context"
	"crypto/sha256"
	"fmt"
	"time"

	"github.com/xraph/nexus/provider"
)

// StreamFrame is one captured chunk plus its arrival offset since the start
// of the recording. OffsetMs is used by paced replay to reproduce the
// original cadence.
type StreamFrame struct {
	Chunk    *provider.StreamChunk
	OffsetMs int64
}

// ReplayMode controls how a cached stream is played back.
type ReplayMode int

const (
	// ReplayBurst emits all cached frames immediately. Fastest; loses the
	// original timing. Default mode for Nexus.
	ReplayBurst ReplayMode = iota

	// ReplayPaced sleeps between frames to reproduce the original cadence.
	// Useful for demos / matching real-time UX.
	ReplayPaced

	// ReplayFastForward applies a divisor to the original gaps.
	// Use ReplayFastForwardN for divisor configuration.
	ReplayFastForward
)

// StreamCacheOptions tunes recording and replay caps.
type StreamCacheOptions struct {
	TTL       time.Duration // entry lifetime; 0 = unbounded (subject to backend)
	MaxFrames int           // abandon recording after this many frames; 0 = unlimited
	MaxBytes  int           // abandon recording when total payload exceeds; 0 = unlimited
	Mode      ReplayMode    // replay strategy on hit
	FFDivisor float64       // ReplayFastForward divisor (default 4x)
}

// StreamCache stores ordered captures of streamed responses, keyed
// independently from the non-streaming Cache. Implementations need not
// support both tiers — a deployment may register a memory stream cache
// even when its standard cache is Redis.
type StreamCache interface {
	GetStream(ctx context.Context, key string) ([]StreamFrame, error)
	SetStream(ctx context.Context, key string, frames []StreamFrame, ttl time.Duration) error
	DeleteStream(ctx context.Context, key string) error
}

// StreamKey derives a deterministic cache key for a streaming request.
// Adds a "stream:" prefix to the standard request key so streaming and
// non-streaming caches don't collide if a single backend hosts both.
func StreamKey(req *provider.CompletionRequest) string {
	base := Key(req)
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "stream:%s", base)
	return fmt.Sprintf("%x", h.Sum(nil))
}
