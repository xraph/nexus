package middlewares

import (
	"context"
	"sync"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

// UsageMiddleware records usage for each request.
//
// For non-streaming responses it reads resp.Completion.Usage immediately and
// records asynchronously. For streaming responses, where Completion is nil
// at handler-return time, it wraps resp.Stream so usage records are emitted
// when the consumer drains and closes the stream — without this, streamed
// traffic silently bypasses billing.
type UsageMiddleware struct {
	usage usage.Service
}

// NewUsage creates a usage tracking middleware.
func NewUsage(u usage.Service) *UsageMiddleware {
	return &UsageMiddleware{usage: u}
}

func (m *UsageMiddleware) Name() string  { return "usage" }
func (m *UsageMiddleware) Priority() int { return 550 } // After everything else

func (m *UsageMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if m.usage == nil {
		return next(ctx)
	}

	start := time.Now()
	resp, err := next(ctx)
	elapsed := time.Since(start)

	rec := &usage.Record{
		ID:        id.NewUsageID(),
		Provider:  pipeline.ProviderName(ctx),
		Latency:   elapsed,
		CreatedAt: time.Now(),
	}

	if req.Completion != nil {
		rec.Model = req.Completion.Model
	}

	if providerName, ok := req.State["provider_name"].(string); ok {
		rec.Provider = providerName
	}

	switch {
	case err != nil:
		rec.StatusCode = 500
		m.recordAsync(rec)
	case resp != nil && resp.Stream != nil:
		// Streaming — defer recording until Close. Wrap the stream so token
		// totals are captured from Stream.Usage() once the upstream finishes
		// (and from any final response synthesised by StreamLifecycle).
		rec.StatusCode = 200
		resp.Stream = &usageRecordingStream{
			inner:   resp.Stream,
			rec:     rec,
			req:     req,
			start:   start,
			recordF: m.recordAsync,
		}
	case resp != nil && resp.Completion != nil:
		rec.StatusCode = 200
		rec.PromptTokens = resp.Completion.Usage.PromptTokens
		rec.CompletionTokens = resp.Completion.Usage.CompletionTokens
		rec.TotalTokens = resp.Completion.Usage.TotalTokens
		rec.Cached = resp.Completion.Cached
		rec.CostUSD = resp.Completion.Cost
		m.recordAsync(rec)
	default:
		rec.StatusCode = 200
		m.recordAsync(rec)
	}

	return resp, err
}

func (m *UsageMiddleware) recordAsync(rec *usage.Record) {
	go func() {
		_ = m.usage.Record(context.Background(), rec) //nolint:errcheck // best-effort async usage recording
	}()
}

// usageRecordingStream is a thin pass-through that records usage when the
// underlying stream is closed. It prefers the merged final response written
// to req.State by StreamLifecycleMiddleware (priority 545); otherwise it
// falls back to inner.Usage().
type usageRecordingStream struct {
	inner provider.Stream
	rec   *usage.Record
	req   *pipeline.Request
	start time.Time

	recordF  func(*usage.Record)
	recorded bool
	mu       sync.Mutex
}

func (s *usageRecordingStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	chunk, err := s.inner.Next(ctx)
	// Capture intermediate usage frames (OpenAI's stream_options.include_usage
	// terminal chunk) so we still record even if the consumer panics before
	// Close runs.
	if chunk != nil && chunk.Usage != nil {
		s.mu.Lock()
		s.rec.PromptTokens = chunk.Usage.PromptTokens
		s.rec.CompletionTokens = chunk.Usage.CompletionTokens
		s.rec.TotalTokens = chunk.Usage.TotalTokens
		s.mu.Unlock()
	}
	return chunk, err
}

func (s *usageRecordingStream) Close() error {
	s.mu.Lock()
	already := s.recorded
	s.recorded = true
	s.mu.Unlock()

	closeErr := s.inner.Close()

	if already {
		return closeErr
	}

	// Prefer the synthesised final response (richer signal — token + cost +
	// model). Fall back to inner.Usage() / per-chunk capture.
	if s.req != nil && s.req.State != nil {
		if v, ok := s.req.State[StateKeyStreamFinalResponse]; ok {
			if final, ok := v.(*provider.CompletionResponse); ok && final != nil {
				if s.rec.PromptTokens == 0 {
					s.rec.PromptTokens = final.Usage.PromptTokens
				}
				if s.rec.CompletionTokens == 0 {
					s.rec.CompletionTokens = final.Usage.CompletionTokens
				}
				if s.rec.TotalTokens == 0 {
					s.rec.TotalTokens = final.Usage.TotalTokens
				}
				if s.rec.Model == "" {
					s.rec.Model = final.Model
				}
				if s.rec.Provider == "" {
					s.rec.Provider = final.Provider
				}
				if s.rec.CostUSD == 0 {
					s.rec.CostUSD = final.Cost
				}
			}
		}
	}

	if u := s.inner.Usage(); u != nil {
		if s.rec.PromptTokens == 0 {
			s.rec.PromptTokens = u.PromptTokens
		}
		if s.rec.CompletionTokens == 0 {
			s.rec.CompletionTokens = u.CompletionTokens
		}
		if s.rec.TotalTokens == 0 {
			s.rec.TotalTokens = u.TotalTokens
		}
	}

	if s.rec.TotalTokens == 0 {
		s.rec.TotalTokens = s.rec.PromptTokens + s.rec.CompletionTokens
	}
	s.rec.Latency = time.Since(s.start)
	s.recordF(s.rec)
	return closeErr
}

func (s *usageRecordingStream) Usage() *provider.Usage { return s.inner.Usage() }
