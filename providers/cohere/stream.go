package cohere

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/xraph/nexus/provider"
)

type cohereStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	model   string
	usage   *provider.Usage
	done    bool
}

func newCohereStream(body io.ReadCloser, model string) *cohereStream {
	return &cohereStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
	}
}

func (s *cohereStream) Next(_ context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var event map[string]any
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		eventType, _ := event["type"].(string) //nolint:errcheck // zero value is fine

		switch eventType {
		case "content-delta":
			delta, _ := event["delta"].(map[string]any) //nolint:errcheck // zero value is fine
			if delta == nil {
				continue
			}
			message, _ := delta["message"].(map[string]any) //nolint:errcheck // zero value is fine
			if message == nil {
				continue
			}
			content, _ := message["content"].(map[string]any) //nolint:errcheck // zero value is fine
			if content == nil {
				continue
			}
			text, _ := content["text"].(string) //nolint:errcheck // zero value is fine

			return &provider.StreamChunk{
				ID:       fmt.Sprintf("cohere-%d", time.Now().UnixNano()),
				Provider: "cohere",
				Model:    s.model,
				Delta: provider.Delta{
					Content: text,
				},
			}, nil

		case "message-end":
			delta, _ := event["delta"].(map[string]any) //nolint:errcheck // zero value is fine
			if delta != nil {
				if u, ok := delta["usage"].(map[string]any); ok {
					tokens, _ := u["tokens"].(map[string]any) //nolint:errcheck // zero value is fine
					if tokens != nil {
						input, _ := tokens["input_tokens"].(float64)   //nolint:errcheck // zero value is fine
						output, _ := tokens["output_tokens"].(float64) //nolint:errcheck // zero value is fine
						s.usage = &provider.Usage{
							PromptTokens:     int(input),
							CompletionTokens: int(output),
							TotalTokens:      int(input + output),
						}
					}
				}
				finishReason, _ := delta["finish_reason"].(string) //nolint:errcheck // zero value is fine
				return &provider.StreamChunk{
					ID:           fmt.Sprintf("cohere-%d", time.Now().UnixNano()),
					Provider:     "cohere",
					Model:        s.model,
					FinishReason: mapCohereFinishReason(finishReason),
				}, nil
			}
		}
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("cohere: stream read error: %w", err)
	}

	s.done = true
	return nil, io.EOF
}

func (s *cohereStream) Close() error {
	s.done = true
	return s.body.Close()
}

func (s *cohereStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*cohereStream)(nil)
