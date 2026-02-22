package openai

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/xraph/nexus/provider"
)

type openAIStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	model   string
	usage   *provider.Usage
	done    bool
}

func newOpenAIStream(body io.ReadCloser, model string) *openAIStream {
	return &openAIStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
	}
}

func (s *openAIStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// SSE format: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		// Check for stream end
		if data == "[DONE]" {
			s.done = true
			return nil, io.EOF
		}

		var chunk struct {
			ID      string `json:"id"`
			Model   string `json:"model"`
			Choices []struct {
				Index int `json:"index"`
				Delta struct {
					Role      string              `json:"role,omitempty"`
					Content   string              `json:"content,omitempty"`
					ToolCalls []provider.ToolCall `json:"tool_calls,omitempty"`
				} `json:"delta"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
			Usage *struct {
				PromptTokens     int `json:"prompt_tokens"`
				CompletionTokens int `json:"completion_tokens"`
				TotalTokens      int `json:"total_tokens"`
			} `json:"usage,omitempty"`
		}

		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return nil, fmt.Errorf("openai: decode stream chunk: %w", err)
		}

		// Capture usage if present (OpenAI sends it in the last chunk)
		if chunk.Usage != nil {
			s.usage = &provider.Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
		}

		if len(chunk.Choices) == 0 {
			continue
		}

		ch := chunk.Choices[0]
		finishReason := ""
		if ch.FinishReason != nil {
			finishReason = *ch.FinishReason
		}

		return &provider.StreamChunk{
			ID:       chunk.ID,
			Provider: "openai",
			Model:    chunk.Model,
			Delta: provider.Delta{
				Role:      ch.Delta.Role,
				Content:   ch.Delta.Content,
				ToolCalls: ch.Delta.ToolCalls,
			},
			FinishReason: finishReason,
		}, nil
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("openai: stream read error: %w", err)
	}

	s.done = true
	return nil, io.EOF
}

func (s *openAIStream) Close() error {
	s.done = true
	return s.body.Close()
}

func (s *openAIStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*openAIStream)(nil)
