package bedrock

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

// bedrockStream implements provider.Stream for Bedrock's converse-with-response-stream.
// Bedrock streams events as SSE-style lines with JSON payloads.
type bedrockStream struct {
	body    io.ReadCloser
	scanner *bufio.Scanner
	model   string
	usage   *provider.Usage
	done    bool
	msgID   string
}

func newBedrockStream(body io.ReadCloser, model string) *bedrockStream {
	return &bedrockStream{
		body:    body,
		scanner: bufio.NewScanner(body),
		model:   model,
		msgID:   fmt.Sprintf("bedrock-%d", time.Now().UnixNano()),
	}
}

// Bedrock stream event envelope types.

type streamEvent struct {
	ContentBlockDelta *contentBlockDeltaEvent `json:"contentBlockDelta,omitempty"`
	ContentBlockStart *contentBlockStartEvent `json:"contentBlockStart,omitempty"`
	ContentBlockStop  *contentBlockStopEvent  `json:"contentBlockStop,omitempty"`
	MessageStart      *messageStartEvent      `json:"messageStart,omitempty"`
	MessageStop       *messageStopEvent       `json:"messageStop,omitempty"`
	Metadata          *metadataEvent          `json:"metadata,omitempty"`
}

type contentBlockDeltaEvent struct {
	Delta             blockDelta `json:"delta"`
	ContentBlockIndex int        `json:"contentBlockIndex"`
}

type blockDelta struct {
	Text    string        `json:"text,omitempty"`
	ToolUse *toolUseDelta `json:"toolUse,omitempty"`
}

type toolUseDelta struct {
	Input string `json:"input,omitempty"`
}

type contentBlockStartEvent struct {
	Start             contentBlockStartBody `json:"start"`
	ContentBlockIndex int                   `json:"contentBlockIndex"`
}

type contentBlockStartBody struct {
	ToolUse *toolUseStart `json:"toolUse,omitempty"`
}

type toolUseStart struct {
	ToolUseID string `json:"toolUseId"`
	Name      string `json:"name"`
}

type contentBlockStopEvent struct {
	ContentBlockIndex int `json:"contentBlockIndex"`
}

type messageStartEvent struct {
	Role string `json:"role,omitempty"`
}

type messageStopEvent struct {
	StopReason string `json:"stopReason"`
}

type metadataEvent struct {
	Usage *converseUsage `json:"usage,omitempty"`
}


func (s *bedrockStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}

	for s.scanner.Scan() {
		line := s.scanner.Text()

		// Skip empty lines and SSE comments.
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Look for SSE "data: " prefix.
		if !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		var event streamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			continue
		}

		// Handle content block delta (text streaming).
		if event.ContentBlockDelta != nil {
			delta := event.ContentBlockDelta.Delta

			// Text delta.
			if delta.Text != "" {
				return &provider.StreamChunk{
					ID:       s.msgID,
					Provider: "bedrock",
					Model:    s.model,
					Delta: provider.Delta{
						Content: delta.Text,
					},
				}, nil
			}

			// Tool use input delta (partial JSON).
			if delta.ToolUse != nil && delta.ToolUse.Input != "" {
				// Tool input fragments are accumulated; emit as content for now.
				// Consumers should use the final tool call from the completion.
				continue
			}
		}

		// Handle content block start (tool use start).
		if event.ContentBlockStart != nil && event.ContentBlockStart.Start.ToolUse != nil {
			tu := event.ContentBlockStart.Start.ToolUse
			return &provider.StreamChunk{
				ID:       s.msgID,
				Provider: "bedrock",
				Model:    s.model,
				Delta: provider.Delta{
					ToolCalls: []provider.ToolCall{{
						ID:   tu.ToolUseID,
						Type: "function",
						Function: provider.ToolCallFunc{
							Name: tu.Name,
						},
					}},
				},
			}, nil
		}

		// Handle message stop.
		if event.MessageStop != nil {
			return &provider.StreamChunk{
				ID:           s.msgID,
				Provider:     "bedrock",
				Model:        s.model,
				FinishReason: mapStopReason(event.MessageStop.StopReason),
			}, nil
		}

		// Handle metadata (usage).
		if event.Metadata != nil && event.Metadata.Usage != nil {
			u := event.Metadata.Usage
			s.usage = &provider.Usage{
				PromptTokens:     u.InputTokens,
				CompletionTokens: u.OutputTokens,
				TotalTokens:      u.TotalTokens,
			}
			continue
		}

		// Message start and other events are ignored.
	}

	if err := s.scanner.Err(); err != nil {
		return nil, fmt.Errorf("bedrock: stream read error: %w", err)
	}

	s.done = true
	return nil, io.EOF
}

func (s *bedrockStream) Close() error {
	s.done = true
	return s.body.Close()
}

func (s *bedrockStream) Usage() *provider.Usage {
	return s.usage
}

// Compile-time check.
var _ provider.Stream = (*bedrockStream)(nil)
