package guard

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"

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
					return nil, &BlockedError{Guard: g.Name(), Phase: PhaseOutput, Reason: result.Reason}
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
			return nil, &BlockedError{Guard: g.Name(), Phase: PhaseOutput, Reason: result.Reason}
		}
	}

	return chunk, nil
}

// OutputGuardedStream runs output-phase guards over a streamed response. The
// strategy decides when the consumer sees the chunks:
//
//   - StrategyBuffer holds every chunk until the stream ends, then checks the
//     whole message (text and tool calls). A block ends the stream with
//     *BlockedError before any chunk is delivered. A rewrite is replayed as a
//     new stream built from the rewritten message: one content delta, one
//     complete tool-call delta per call, the finish reason and the usage. It
//     carries no reasoning and no native content. Otherwise the original
//     chunks are replayed.
//   - StrategyPassthrough delivers chunks as they arrive and checks the
//     message when the provider ends the stream. A block replaces io.EOF with
//     *BlockedError, so a consumer that runs tool calls only after a clean end
//     never runs a blocked one. A rewrite cannot be applied to text already
//     delivered: the stream ends with io.EOF and the change is lost.
//   - StrategyChunkwise checks every chunk with the registered StreamGuards
//     before delivering it, then checks the message at the end as
//     StrategyPassthrough does.
//
// StreamGuards also check each chunk under StrategyBuffer, before anything is
// delivered. StrategyPassthrough does not run them.
//
// Native content holds the reply's original text and thinking. Under the
// streaming strategies the chunks that carry it are held back until the
// verdict: a block never delivers them, and a rewrite delivers them without
// it.
type OutputGuardedStream struct {
	inner       provider.Stream
	svc         Service
	input       CheckInput
	strategy    StreamStrategy
	chunkGuards []StreamGuard

	acc     *provider.Accumulator
	held    []*provider.StreamChunk // waiting for the verdict
	ready   []*provider.StreamChunk // cleared for delivery
	holding bool                    // native content was seen: hold the rest
	end     error                   // what Next returns once ready is empty

	mu    sync.Mutex
	usage *provider.Usage

	closeOnce sync.Once
	closeErr  error
}

// NewOutputGuardedStream wraps inner so that svc's output-phase guards check
// the streamed message. input carries the tenant and metadata the guards
// see; its Messages are replaced with the streamed message. An empty or
// unknown strategy is StrategyBuffer. A nil svc runs no output guards.
func NewOutputGuardedStream(inner provider.Stream, svc Service, input *CheckInput, strategy StreamStrategy) *OutputGuardedStream {
	s := &OutputGuardedStream{inner: inner, svc: svc, strategy: strategy, acc: provider.NewAccumulator()}
	if input != nil {
		s.input = *input
	}
	switch strategy {
	case StrategyPassthrough, StrategyChunkwise:
	default:
		s.strategy = StrategyBuffer
	}
	return s
}

// WithStreamGuards adds guards that check every chunk under StrategyBuffer
// and StrategyChunkwise.
func (s *OutputGuardedStream) WithStreamGuards(guards ...StreamGuard) *OutputGuardedStream {
	s.chunkGuards = append(s.chunkGuards, guards...)
	return s
}

// Next returns the next chunk the strategy has cleared, or the verdict.
func (s *OutputGuardedStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	for {
		if len(s.ready) > 0 {
			c := s.ready[0]
			s.ready = s.ready[1:]
			return c, nil
		}
		if s.end != nil {
			return nil, s.end
		}
		chunk, err := s.inner.Next(ctx)
		if errors.Is(err, io.EOF) {
			s.verdict(ctx)
			continue
		}
		if err != nil {
			s.end = err
			return nil, err
		}
		if chunk == nil {
			continue
		}
		if chunk.Kind == provider.EventHeartbeat {
			// A keepalive carries no content; holding it would let an idle
			// connection time out while the stream is buffered.
			return chunk, nil
		}
		s.observe(chunk)
		if s.strategy != StrategyPassthrough {
			if blocked, err := s.checkChunk(ctx, chunk); err != nil || blocked != nil {
				s.stop(blocked, err)
				continue
			}
		}
		if chunk.Delta.Native != nil {
			s.holding = true
		}
		if s.strategy == StrategyBuffer || s.holding {
			s.held = append(s.held, chunk)
			continue
		}
		return chunk, nil
	}
}

// Close closes the upstream stream once.
func (s *OutputGuardedStream) Close() error {
	s.closeOnce.Do(func() { s.closeErr = s.inner.Close() })
	return s.closeErr
}

// Usage returns the usage the stream reported in its chunks, or the
// upstream stream's own.
func (s *OutputGuardedStream) Usage() *provider.Usage {
	s.mu.Lock()
	u := s.usage
	s.mu.Unlock()
	if u != nil {
		return u
	}
	return s.inner.Usage()
}

// observe adds a chunk to the message the output guards will check.
func (s *OutputGuardedStream) observe(c *provider.StreamChunk) {
	s.acc.Add(c)
	if c.Usage != nil {
		s.mu.Lock()
		s.usage = c.Usage
		s.mu.Unlock()
	}
}

// checkChunk runs the stream guards over one chunk.
func (s *OutputGuardedStream) checkChunk(ctx context.Context, c *provider.StreamChunk) (*BlockedError, error) {
	for _, g := range s.chunkGuards {
		r, err := g.CheckChunk(ctx, c)
		if err != nil {
			return nil, err
		}
		if r != nil && r.Blocked {
			return s.blocked(g.Name(), r.Reason), nil
		}
	}
	return nil, nil
}

// verdict checks the whole message once the provider has ended the stream
// and decides what the consumer gets.
func (s *OutputGuardedStream) verdict(ctx context.Context) {
	resp := s.acc.Finalize(s.inner.Usage)
	if s.svc == nil {
		s.release(s.held, io.EOF)
		return
	}
	in := s.input
	in.Messages = []provider.Message{resp.Choices[0].Message}
	r, err := s.svc.CheckPhase(ctx, PhaseOutput, &in)
	switch {
	case err != nil:
		s.stop(nil, err)
	case r == nil:
		s.release(s.held, io.EOF)
	case r.Blocked:
		s.stop(s.blocked(r.Guard, r.Reason), nil)
	case r.Modified && s.strategy == StrategyBuffer && len(r.Messages) > 0:
		s.release(replay(resp, &r.Messages[0]), io.EOF)
	case r.Modified:
		s.release(withoutNative(s.held), io.EOF)
	default:
		s.release(s.held, io.EOF)
	}
}

// release clears chunks for delivery, then ends the stream with end.
func (s *OutputGuardedStream) release(chunks []*provider.StreamChunk, end error) {
	s.held = nil
	s.ready = append(s.ready, chunks...)
	s.end = end
}

// stop ends the stream with a block or an error, delivers nothing it held
// and closes the upstream stream.
func (s *OutputGuardedStream) stop(blocked *BlockedError, err error) {
	s.held = nil
	if blocked != nil {
		s.end = blocked
	} else {
		s.end = err
	}
	// The block or error is what the consumer needs to see. A failure to
	// close is kept for Close to return.
	s.closeOnce.Do(func() { s.closeErr = s.inner.Close() })
}

// blocked builds the output block, with what the stream has consumed so far.
func (s *OutputGuardedStream) blocked(name, reason string) *BlockedError {
	resp := s.acc.Finalize(s.inner.Usage)
	e := &BlockedError{Guard: name, Phase: PhaseOutput, Reason: reason, Model: resp.Model, Provider: resp.Provider}
	if u := resp.Usage; u != (provider.Usage{}) {
		e.Usage = &u
	}
	return e
}

// replay builds the stream a buffered rewrite is delivered as.
func replay(resp *provider.CompletionResponse, msg *provider.Message) []*provider.StreamChunk {
	base := provider.StreamChunk{ID: resp.ID, Provider: resp.Provider, Model: resp.Model}
	role := msg.Role
	if role == "" {
		role = "assistant"
	}
	text := base
	text.Delta = provider.Delta{Role: role, Content: textOf(msg.Content)}
	chunks := []*provider.StreamChunk{&text}

	// One delta per call keeps each call whole. Calls without an id would
	// merge by slot index when read back, so they travel in one delta.
	withIDs := true
	for i := range msg.ToolCalls {
		withIDs = withIDs && msg.ToolCalls[i].ID != ""
	}
	if withIDs {
		for i := range msg.ToolCalls {
			c := base
			c.Kind = provider.EventToolCallDelta
			c.Delta = provider.Delta{ToolCalls: []provider.ToolCall{msg.ToolCalls[i]}}
			chunks = append(chunks, &c)
		}
	} else {
		c := base
		c.Kind = provider.EventToolCallDelta
		c.Delta = provider.Delta{ToolCalls: append([]provider.ToolCall(nil), msg.ToolCalls...)}
		chunks = append(chunks, &c)
	}

	if fr := resp.Choices[0].FinishReason; fr != "" {
		c := base
		c.FinishReason = fr
		chunks = append(chunks, &c)
	}
	if u := resp.Usage; u != (provider.Usage{}) {
		c := base
		c.Kind = provider.EventUsage
		c.Usage = &u
		chunks = append(chunks, &c)
	}
	return chunks
}

// withoutNative returns chunks with native content removed, copying each
// chunk it changes rather than editing one the upstream stream may share.
func withoutNative(chunks []*provider.StreamChunk) []*provider.StreamChunk {
	out := make([]*provider.StreamChunk, len(chunks))
	for i, c := range chunks {
		if c.Delta.Native == nil {
			out[i] = c
			continue
		}
		cp := *c
		cp.Delta.Native = nil
		out[i] = &cp
	}
	return out
}

// textOf is a message's text: the string itself, or its text parts joined.
func textOf(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case []provider.ContentPart:
		var b strings.Builder
		for _, p := range v {
			b.WriteString(p.Text)
		}
		return b.String()
	}
	return ""
}
