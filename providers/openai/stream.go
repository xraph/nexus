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

	// stopAfterFunc is the runtime hook registered with the request context;
	// firing it tears the upstream TCP connection so a canceled ctx aborts
	// the stream without waiting for the next SSE line.
	stopAfterFunc func() bool
}

func newOpenAIStream(ctx context.Context, body io.ReadCloser, model string) *openAIStream {
	s := &openAIStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
	}
	if ctx != nil {
		s.stopAfterFunc = context.AfterFunc(ctx, func() {
			_ = body.Close()
		})
	}
	return s
}

func (s *openAIStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
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

		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

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
					Role             string              `json:"role,omitempty"`
					Content          string              `json:"content,omitempty"`
					Refusal          string              `json:"refusal,omitempty"`
					Reasoning        string              `json:"reasoning,omitempty"`
					ReasoningContent string              `json:"reasoning_content,omitempty"`
					ToolCalls        []provider.ToolCall `json:"tool_calls,omitempty"`
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

		if chunk.Usage != nil {
			s.usage = &provider.Usage{
				PromptTokens:     chunk.Usage.PromptTokens,
				CompletionTokens: chunk.Usage.CompletionTokens,
				TotalTokens:      chunk.Usage.TotalTokens,
			}
			// OpenAI emits a final `usage` chunk with empty choices when
			// stream_options.include_usage=true. Surface it as EventUsage
			// so middleware (billing, accumulators) can capture it without
			// waiting for the iterator to drain.
			if len(chunk.Choices) == 0 {
				return &provider.StreamChunk{
					ID:       chunk.ID,
					Provider: "openai",
					Model:    chunk.Model,
					Kind:     provider.EventUsage,
					Usage:    s.usage,
				}, nil
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

		reasoning := ch.Delta.Reasoning
		if reasoning == "" {
			reasoning = ch.Delta.ReasoningContent
		}

		kind := provider.EventDelta
		if reasoning != "" && ch.Delta.Content == "" && len(ch.Delta.ToolCalls) == 0 {
			kind = provider.EventReasoning
		} else if len(ch.Delta.ToolCalls) > 0 && ch.Delta.Content == "" {
			kind = provider.EventToolCallDelta
		}

		return &provider.StreamChunk{
			ID:       chunk.ID,
			Provider: "openai",
			Model:    chunk.Model,
			Kind:     kind,
			Delta: provider.Delta{
				Role:      ch.Delta.Role,
				Content:   ch.Delta.Content,
				Reasoning: reasoning,
				Refusal:   ch.Delta.Refusal,
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
	if s.stopAfterFunc != nil {
		s.stopAfterFunc()
	}
	return s.body.Close()
}

func (s *openAIStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*openAIStream)(nil)
