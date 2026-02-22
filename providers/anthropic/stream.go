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
}

func newAnthropicStream(body io.ReadCloser, model string) *anthropicStream {
	return &anthropicStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
	}
}

func (s *anthropicStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if strings.HasPrefix(line, "event: ") {
			event := strings.TrimPrefix(line, "event: ")
			if event == "message_stop" {
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

		eventType, _ := raw["type"].(string)

		switch eventType {
		case "message_start":
			// Extract message ID
			if msg, ok := raw["message"].(map[string]any); ok {
				s.msgID, _ = msg["id"].(string)
			}
			continue

		case "content_block_delta":
			delta, _ := raw["delta"].(map[string]any)
			if delta == nil {
				continue
			}
			deltaType, _ := delta["type"].(string)

			switch deltaType {
			case "text_delta":
				text, _ := delta["text"].(string)
				return &provider.StreamChunk{
					ID:       s.msgID,
					Provider: "anthropic",
					Model:    s.model,
					Delta: provider.Delta{
						Content: text,
					},
				}, nil
			}

		case "message_delta":
			// Final delta with stop_reason and usage
			if u, ok := raw["usage"].(map[string]any); ok {
				outputTokens := int(u["output_tokens"].(float64))
				s.usage = &provider.Usage{
					CompletionTokens: outputTokens,
				}
			}
			stopReason, _ := raw["stop_reason"].(string)
			if stopReason == "" {
				if delta, ok := raw["delta"].(map[string]any); ok {
					stopReason, _ = delta["stop_reason"].(string)
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
	return s.body.Close()
}

func (s *anthropicStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*anthropicStream)(nil)
