package anthropic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/xraph/nexus/provider"
)

type anthropicStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	model   string
	usage   *provider.Usage
	done    bool
	msgID   string
	// inputTokens captured from message_start.
	inputTokens int

	// toolBlocks maps Anthropic content-block indexes to in-flight tool
	// metadata (id + name). content_block_start fires first, then a stream
	// of input_json_delta chunks under the same index — we need the id+name
	// to attach to each delta we emit.
	toolBlocks map[int]toolBlockState

	stopAfterFunc func() bool
}

type toolBlockState struct {
	id   string
	name string
}

func newAnthropicStream(ctx context.Context, body io.ReadCloser, model string) *anthropicStream {
	s := &anthropicStream{
		body:       body,
		scanner:    bufio.NewScanner(body),
		model:      model,
		toolBlocks: make(map[int]toolBlockState),
	}
	if ctx != nil {
		s.stopAfterFunc = context.AfterFunc(ctx, func() {
			_ = body.Close()
		})
	}
	return s
}

func (s *anthropicStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		if ctx != nil {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			default:
			}
		}

		line := s.scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			evt := strings.TrimPrefix(line, "event: ")
			if evt == "message_stop" {
				s.done = true
				return nil, io.EOF
			}
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var raw map[string]any
		if err := json.Unmarshal([]byte(data), &raw); err != nil {
			continue
		}

		eventType, _ := raw["type"].(string) //nolint:errcheck // zero value is fine

		switch eventType {
		case "message_start":
			if msg, ok := raw["message"].(map[string]any); ok {
				s.msgID, _ = msg["id"].(string) //nolint:errcheck // zero value is fine
				if u, ok := msg["usage"].(map[string]any); ok {
					if f, ok := u["input_tokens"].(float64); ok {
						s.inputTokens = int(f)
						s.usage = &provider.Usage{PromptTokens: s.inputTokens}
					}
				}
			}
			return &provider.StreamChunk{
				ID:       s.msgID,
				Provider: "anthropic",
				Model:    s.model,
				Kind:     provider.EventMessageStart,
				Usage:    s.usage,
			}, nil

		case "ping":
			continue

		case "content_block_start":
			// Capture tool_use metadata for subsequent input_json_delta chunks.
			idx := indexOf(raw)
			if block, ok := raw["content_block"].(map[string]any); ok {
				blockType, _ := block["type"].(string) //nolint:errcheck // zero value is fine
				if blockType == "tool_use" {
					id, _ := block["id"].(string)     //nolint:errcheck // zero value is fine
					name, _ := block["name"].(string) //nolint:errcheck // zero value is fine
					s.toolBlocks[idx] = toolBlockState{id: id, name: name}
				}
			}
			continue

		case "content_block_stop":
			continue

		case "content_block_delta":
			delta, _ := raw["delta"].(map[string]any) //nolint:errcheck // zero value is fine
			if delta == nil {
				continue
			}
			deltaType, _ := delta["type"].(string) //nolint:errcheck // zero value is fine

			switch deltaType {
			case "text_delta":
				text, _ := delta["text"].(string) //nolint:errcheck // zero value is fine
				return &provider.StreamChunk{
					ID:       s.msgID,
					Provider: "anthropic",
					Model:    s.model,
					Delta:    provider.Delta{Content: text},
				}, nil

			case "thinking_delta":
				txt, _ := delta["thinking"].(string) //nolint:errcheck // zero value is fine
				return &provider.StreamChunk{
					ID:       s.msgID,
					Provider: "anthropic",
					Model:    s.model,
					Kind:     provider.EventReasoning,
					Delta:    provider.Delta{Reasoning: txt},
				}, nil

			case "input_json_delta":
				partial, _ := delta["partial_json"].(string) //nolint:errcheck // zero value is fine
				idx := indexOf(raw)
				blk := s.toolBlocks[idx]
				return &provider.StreamChunk{
					ID:       s.msgID,
					Provider: "anthropic",
					Model:    s.model,
					Kind:     provider.EventToolCallDelta,
					Delta: provider.Delta{
						ToolCalls: []provider.ToolCall{{
							ID:   blk.id,
							Type: "function",
							Function: provider.ToolCallFunc{
								Name:      blk.name,
								Arguments: partial,
							},
						}},
					},
				}, nil

			case "citations_delta":
				if c, ok := delta["citation"].(map[string]any); ok {
					cit := provider.Citation{}
					cit.URL, _ = c["url"].(string)     //nolint:errcheck // zero value is fine
					cit.Title, _ = c["title"].(string) //nolint:errcheck // zero value is fine
					// Anthropic has used both `cited_text` (current) and `text`
					// (older shapes). Prefer canonical, fall back gracefully.
					if q, ok := c["cited_text"].(string); ok && q != "" {
						cit.Quoted = q
					} else if q, ok := c["text"].(string); ok {
						cit.Quoted = q
					}
					if f, ok := c["start_char_index"].(float64); ok {
						cit.StartIdx = int(f)
					}
					if f, ok := c["end_char_index"].(float64); ok {
						cit.EndIdx = int(f)
					}
					return &provider.StreamChunk{
						ID:       s.msgID,
						Provider: "anthropic",
						Model:    s.model,
						Kind:     provider.EventCitation,
						Delta:    provider.Delta{Citations: []provider.Citation{cit}},
					}, nil
				}
				continue
			}

		case "message_delta":
			// Final delta with stop_reason and usage.
			if u, ok := raw["usage"].(map[string]any); ok {
				out, _ := u["output_tokens"].(float64) //nolint:errcheck // zero value is fine
				outputTokens := int(out)
				promptTokens := s.inputTokens
				if in, ok := u["input_tokens"].(float64); ok {
					promptTokens = int(in)
				}
				total := promptTokens + outputTokens
				s.usage = &provider.Usage{
					PromptTokens:     promptTokens,
					CompletionTokens: outputTokens,
					TotalTokens:      total,
				}
			}
			stopReason, _ := raw["stop_reason"].(string) //nolint:errcheck // zero value is fine
			if stopReason == "" {
				if delta, ok := raw["delta"].(map[string]any); ok {
					stopReason, _ = delta["stop_reason"].(string) //nolint:errcheck // zero value is fine
				}
			}
			return &provider.StreamChunk{
				ID:           s.msgID,
				Provider:     "anthropic",
				Model:        s.model,
				FinishReason: mapStopReason(stopReason),
			}, nil
		}
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("anthropic: stream read error: %w", err)
	}

	s.done = true
	return nil, io.EOF
}

func (s *anthropicStream) Close() error {
	s.done = true
	if s.stopAfterFunc != nil {
		s.stopAfterFunc()
	}
	return s.body.Close()
}

func (s *anthropicStream) Usage() *provider.Usage {
	return s.usage
}

// indexOf reads the top-level `index` field from an Anthropic SSE event.
func indexOf(raw map[string]any) int {
	if f, ok := raw["index"].(float64); ok {
		return int(f)
	}
	return 0
}

// Compile-time check.
var _ provider.Stream = (*anthropicStream)(nil)
