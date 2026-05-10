package httpstream

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/xraph/nexus/provider"
)

// SSEOpenAIEncoder emits events in the OpenAI chat.completion.chunk envelope
// over Server-Sent Events. The intent is drop-in compatibility with OpenAI
// SDKs (openai-python, openai-go, openai-node, etc.).
//
// Tool-call deltas are forwarded with their `index`/`function.name`/
// `function.arguments` fields preserved. Reasoning is surfaced as
// `delta.reasoning_content` (Anthropic-on-OpenAI extension already used by
// DeepSeek and other gateways) so OpenAI clients silently ignore it while
// nexus-aware clients can read it.
//
// Final usage chunk follows OpenAI's stream_options.include_usage shape:
//
//	data: {"id":"…","object":"chat.completion.chunk","choices":[],"usage":{...}}
//	data: [DONE]
type SSEOpenAIEncoder struct{}

// NewSSEOpenAIEncoder returns the default OpenAI-compatible SSE encoder.
func NewSSEOpenAIEncoder() *SSEOpenAIEncoder { return &SSEOpenAIEncoder{} }

func (e *SSEOpenAIEncoder) ContentType() string { return "text/event-stream" }

func (e *SSEOpenAIEncoder) WriteHeaders(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", e.ContentType())
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
}

func (e *SSEOpenAIEncoder) EncodeEvent(w io.Writer, ev *StreamEvent) error {
	if ev == nil {
		return nil
	}
	if ev.Type == EventTypeError {
		return e.EncodeError(w, ev.Err)
	}

	envelope := openAIChunkEnvelope{
		ID:      ev.ID,
		Object:  "chat.completion.chunk",
		Model:   ev.Model,
		Choices: []openAIChoice{},
	}

	switch ev.Type {
	case EventTypeUsage:
		// Final usage frame: choices empty, usage populated. Followed by
		// [DONE] from End().
		if ev.Usage != nil {
			envelope.Usage = &openAIUsage{
				PromptTokens:     ev.Usage.PromptTokens,
				CompletionTokens: ev.Usage.CompletionTokens,
				TotalTokens:      ev.Usage.TotalTokens,
				ThinkingTokens:   ev.Usage.ThinkingTokens,
			}
		}
	case EventTypeHeartbeat:
		// SSE comment — invisible to JSON parsers.
		_, err := fmt.Fprintf(w, ": ping\n\n")
		return err
	default:
		choice := openAIChoice{Index: 0}
		if ev.FinishReason != "" {
			fr := ev.FinishReason
			choice.FinishReason = &fr
		}
		if ev.Delta != nil {
			choice.Delta = openAIDelta{
				Role:             ev.Delta.Role,
				Content:          ev.Delta.Content,
				ReasoningContent: ev.Delta.Reasoning,
				Refusal:          ev.Delta.Refusal,
				ToolCalls:        ev.Delta.ToolCalls,
			}
			if ev.Delta.Audio != nil {
				choice.Delta.Audio = audioDeltaJSON(ev.Delta.Audio)
			}
		}
		if ev.Type == EventTypeAudio && ev.Audio != nil {
			choice.Delta.Audio = audioDeltaJSON(ev.Audio)
		}
		envelope.Choices = []openAIChoice{choice}
	}

	data, err := json.Marshal(envelope)
	if err != nil {
		return fmt.Errorf("httpstream: marshal sse chunk: %w", err)
	}
	_, err = fmt.Fprintf(w, "data: %s\n\n", data)
	return err
}

func (e *SSEOpenAIEncoder) EncodeError(w io.Writer, werr *WireError) error {
	if werr == nil {
		return nil
	}
	body := struct {
		Error *WireError `json:"error"`
	}{Error: werr}
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "event: error\ndata: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func (e *SSEOpenAIEncoder) Heartbeat(w io.Writer) error {
	_, err := fmt.Fprintf(w, ": ping\n\n")
	return err
}

func (e *SSEOpenAIEncoder) End(w io.Writer) error {
	_, err := fmt.Fprintf(w, "data: [DONE]\n\n")
	return err
}

// openAIChunkEnvelope is the on-the-wire shape of an OpenAI streamed chunk.
type openAIChunkEnvelope struct {
	ID      string         `json:"id"`
	Object  string         `json:"object"`
	Model   string         `json:"model,omitempty"`
	Choices []openAIChoice `json:"choices"`
	Usage   *openAIUsage   `json:"usage,omitempty"`
}

type openAIChoice struct {
	Index        int         `json:"index"`
	Delta        openAIDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type openAIDelta struct {
	Role             string              `json:"role,omitempty"`
	Content          string              `json:"content,omitempty"`
	ReasoningContent string              `json:"reasoning_content,omitempty"`
	Refusal          string              `json:"refusal,omitempty"`
	ToolCalls        []provider.ToolCall `json:"tool_calls,omitempty"`
	Audio            *openAIAudio        `json:"audio,omitempty"`
}

type openAIAudio struct {
	Format     string `json:"format,omitempty"`
	SampleRate int    `json:"sample_rate,omitempty"`
	Data       string `json:"data,omitempty"`
	Transcript string `json:"transcript,omitempty"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
	ThinkingTokens   int `json:"thinking_tokens,omitempty"`
}

func audioDeltaJSON(a *provider.AudioChunk) *openAIAudio {
	if a == nil {
		return nil
	}
	out := &openAIAudio{
		Format:     a.Format,
		SampleRate: a.SampleRate,
		Transcript: a.Transcript,
	}
	if len(a.Data) > 0 {
		out.Data = base64.StdEncoding.EncodeToString(a.Data)
	}
	return out
}
