package provider

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"
)

// Accumulate drains a Stream and merges its chunks into a single
// CompletionResponse — the same shape Provider.Complete would have produced.
//
// Merge rules:
//   - Content: concatenated into Choices[0].Message.Content.
//   - Reasoning: concatenated into ThinkingContent.
//   - ToolCalls: merged by ID first, falling back to slot index. function.name
//     keeps the first non-empty value; function.arguments are concatenated.
//   - Citations: appended.
//   - Audio: bytes concatenated; latest non-empty Transcript wins.
//   - Image: last non-Partial chunk wins; otherwise the latest seen.
//   - Usage: prefer the EventUsage / final-delta chunk; otherwise s.Usage().
//   - FinishReason: last non-empty across chunks.
//
// Accumulate stops on io.EOF and closes the stream. It does NOT close on
// error — callers may inspect the partial response and handle cleanup.
func Accumulate(ctx context.Context, s Stream) (*CompletionResponse, error) {
	if s == nil {
		return nil, errors.New("provider: accumulate: nil stream")
	}

	acc := newAccumulator()
	for {
		select {
		case <-ctx.Done():
			return acc.finalize(s), ctx.Err()
		default:
		}

		chunk, err := s.Next(ctx)
		if errors.Is(err, io.EOF) {
			return acc.finalize(s), nil
		}
		if err != nil {
			return acc.finalize(s), err
		}
		if chunk == nil {
			continue
		}
		acc.add(chunk)
	}
}

// NewAccumulator creates an incremental accumulator that callers can feed
// chunks into one at a time, then call Finalize to produce the merged
// CompletionResponse. Useful for middleware that needs to forward chunks to
// the consumer while also assembling the merged response for hooks/cache.
func NewAccumulator() *Accumulator {
	return &Accumulator{inner: newAccumulator()}
}

// Accumulator is the public, incremental form of the merge logic.
type Accumulator struct{ inner *streamAccumulator }

// Add merges a single chunk's contributions into the in-progress response.
// Safe to call with a nil chunk (no-op).
func (a *Accumulator) Add(c *StreamChunk) {
	if a == nil || a.inner == nil || c == nil {
		return
	}
	a.inner.add(c)
}

// Finalize returns the merged CompletionResponse.
// If usageFallback is non-nil and no usage chunk was seen, its return value is
// used. Pass `s.Usage` (the bound method) to fall back to the underlying
// stream's terminal Usage().
func (a *Accumulator) Finalize(usageFallback func() *Usage) *CompletionResponse {
	if a == nil || a.inner == nil {
		return &CompletionResponse{}
	}
	if usageFallback != nil && a.inner.usage == nil {
		if u := usageFallback(); u != nil {
			a.inner.usage = u
		}
	}
	return a.inner.finalize(nil)
}

// streamAccumulator merges chunks into a CompletionResponse.
//
// Used by Accumulate and by middleware that needs the merged final response
// (StreamLifecycle, UsageMiddleware, cache record path).
type streamAccumulator struct {
	id       string
	provider string
	model    string
	created  time.Time

	contentSB   strings.Builder
	reasoningSB strings.Builder
	transcript  string

	role         string
	finishReason string

	// Tool-call slots keyed by index, with a parallel ID lookup.
	toolByIndex map[int]*ToolCall
	toolByID    map[string]*ToolCall
	toolOrder   []int // preserve emit order

	citations []Citation
	audioBuf  []byte
	audioFmt  string
	audioRate int
	finalImg  *ImageChunk

	usage *Usage
}

func newAccumulator() *streamAccumulator {
	return &streamAccumulator{
		toolByIndex: make(map[int]*ToolCall),
		toolByID:    make(map[string]*ToolCall),
	}
}

func (a *streamAccumulator) add(c *StreamChunk) {
	if a.id == "" && c.ID != "" {
		a.id = c.ID
	}
	if a.provider == "" && c.Provider != "" {
		a.provider = c.Provider
	}
	if a.model == "" && c.Model != "" {
		a.model = c.Model
	}
	if a.created.IsZero() && c.Created > 0 {
		a.created = time.UnixMilli(c.Created)
	}
	if c.FinishReason != "" {
		a.finishReason = c.FinishReason
	}

	switch c.Kind {
	case EventUsage:
		if c.Usage != nil {
			a.usage = c.Usage
		}
		return
	case EventError, EventHeartbeat:
		return
	}

	d := c.Delta
	if d.Role != "" {
		a.role = d.Role
	}
	if d.Content != "" {
		a.contentSB.WriteString(d.Content)
	}
	if d.Reasoning != "" {
		a.reasoningSB.WriteString(d.Reasoning)
	}
	if d.Transcript != "" {
		a.transcript = d.Transcript
	}
	if len(d.Citations) > 0 {
		a.citations = append(a.citations, d.Citations...)
	}
	if d.Audio != nil {
		if d.Audio.Format != "" {
			a.audioFmt = d.Audio.Format
		}
		if d.Audio.SampleRate != 0 {
			a.audioRate = d.Audio.SampleRate
		}
		a.audioBuf = append(a.audioBuf, d.Audio.Data...)
		if d.Audio.Transcript != "" {
			a.transcript = d.Audio.Transcript
		}
	}
	if d.Image != nil {
		if !d.Image.Partial {
			img := *d.Image
			a.finalImg = &img
		} else if a.finalImg == nil {
			img := *d.Image
			a.finalImg = &img
		}
	}
	for i := range d.ToolCalls {
		a.mergeTool(i, &d.ToolCalls[i])
	}

	if c.Usage != nil {
		a.usage = c.Usage
	}
}

func (a *streamAccumulator) mergeTool(idx int, in *ToolCall) {
	var slot *ToolCall

	if in.ID != "" {
		if existing, ok := a.toolByID[in.ID]; ok {
			slot = existing
		}
	}
	if slot == nil {
		if existing, ok := a.toolByIndex[idx]; ok {
			slot = existing
		}
	}
	if slot == nil {
		copyIn := *in
		slot = &copyIn
		a.toolByIndex[idx] = slot
		a.toolOrder = append(a.toolOrder, idx)
		if in.ID != "" {
			a.toolByID[in.ID] = slot
		}
		return
	}

	if slot.ID == "" && in.ID != "" {
		slot.ID = in.ID
		a.toolByID[in.ID] = slot
	}
	if slot.Type == "" && in.Type != "" {
		slot.Type = in.Type
	}
	if slot.Function.Name == "" && in.Function.Name != "" {
		slot.Function.Name = in.Function.Name
	}
	slot.Function.Arguments += in.Function.Arguments
}

func (a *streamAccumulator) finalize(s Stream) *CompletionResponse {
	created := a.created
	if created.IsZero() {
		created = time.Now()
	}

	role := a.role
	if role == "" {
		role = "assistant"
	}

	tools := make([]ToolCall, 0, len(a.toolOrder))
	for _, idx := range a.toolOrder {
		if tc, ok := a.toolByIndex[idx]; ok {
			tools = append(tools, *tc)
		}
	}

	resp := &CompletionResponse{
		ID:       a.id,
		Provider: a.provider,
		Model:    a.model,
		Created:  created,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:      role,
					Content:   a.contentSB.String(),
					ToolCalls: tools,
				},
				FinishReason: a.finishReason,
			},
		},
		ThinkingContent: a.reasoningSB.String(),
	}

	if a.usage != nil {
		resp.Usage = *a.usage
	} else if s != nil {
		if u := s.Usage(); u != nil {
			resp.Usage = *u
		}
	}
	if resp.Usage.ThinkingTokens > 0 {
		resp.ThinkingTokens = resp.Usage.ThinkingTokens
	}

	if len(a.citations) > 0 || len(a.audioBuf) > 0 || a.finalImg != nil || a.transcript != "" {
		resp.State = make(map[string]any, 4)
		if len(a.citations) > 0 {
			resp.State["citations"] = a.citations
		}
		if len(a.audioBuf) > 0 {
			resp.State["audio"] = AudioChunk{
				Format:     a.audioFmt,
				SampleRate: a.audioRate,
				Data:       a.audioBuf,
				Transcript: a.transcript,
			}
		}
		if a.finalImg != nil {
			resp.State["image"] = *a.finalImg
		}
		if a.transcript != "" && len(a.audioBuf) == 0 {
			resp.State["transcript"] = a.transcript
		}
	}

	return resp
}
