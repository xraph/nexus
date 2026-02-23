package gemini

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

type geminiStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	model   string
	usage   *provider.Usage
	done    bool
}

func newGeminiStream(body io.ReadCloser, model string) *geminiStream {
	return &geminiStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
	}
}

func (s *geminiStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Skip empty lines and comments.
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// SSE format: "data: {...}"
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var resp geminiResponse
		if err := json.Unmarshal([]byte(data), &resp); err != nil {
			return nil, fmt.Errorf("gemini: decode stream chunk: %w", err)
		}

		// Capture usage if present.
		if resp.UsageMetadata != nil {
			s.usage = &provider.Usage{
				PromptTokens:     resp.UsageMetadata.PromptTokenCount,
				CompletionTokens: resp.UsageMetadata.CandidatesTokenCount,
				TotalTokens:      resp.UsageMetadata.TotalTokenCount,
			}
		}

		if len(resp.Candidates) == 0 {
			continue
		}

		candidate := resp.Candidates[0]
		var content string
		var toolCalls []provider.ToolCall

		for _, part := range candidate.Content.Parts {
			if part.Text != "" {
				content += part.Text
			}
			if part.FunctionCall != nil {
				argsJSON, _ := json.Marshal(part.FunctionCall.Args)
				toolCalls = append(toolCalls, provider.ToolCall{
					ID:   fmt.Sprintf("call_%d", len(toolCalls)),
					Type: "function",
					Function: provider.ToolCallFunc{
						Name:      part.FunctionCall.Name,
						Arguments: string(argsJSON),
					},
				})
			}
		}

		finishReason := ""
		if candidate.FinishReason != "" {
			finishReason = mapFinishReason(candidate.FinishReason)
		}

		return &provider.StreamChunk{
			ID:       fmt.Sprintf("gemini-%d", time.Now().UnixNano()),
			Provider: "gemini",
			Model:    s.model,
			Delta: provider.Delta{
				Content:   content,
				ToolCalls: toolCalls,
			},
			FinishReason: finishReason,
		}, nil
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("gemini: stream read error: %w", err)
	}

	s.done = true
	return nil, io.EOF
}

func (s *geminiStream) Close() error {
	s.done = true
	return s.body.Close()
}

func (s *geminiStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*geminiStream)(nil)
