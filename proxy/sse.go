package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/xraph/nexus/provider"
)

// streamSSE writes Server-Sent Events for a streaming response.
// This is the OpenAI-compatible SSE format:
//
//	data: {"id":"...","object":"chat.completion.chunk","choices":[...]}
//	data: [DONE]
func streamSSE(ctx context.Context, w http.ResponseWriter, stream provider.Stream, model string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal_error", "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Transfer-Encoding", "chunked")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			// Write error as SSE event
			errData, _ := json.Marshal(map[string]any{ //nolint:errcheck // best-effort error SSE event
				"error": map[string]string{
					"message": err.Error(),
					"type":    "stream_error",
				},
			})
			_, _ = fmt.Fprintf(w, "data: %s\n\n", errData)
			flusher.Flush()
			break
		}

		// Convert chunk to OpenAI SSE format
		sseChunk := openAIStreamChunk{
			ID:     chunk.ID,
			Object: "chat.completion.chunk",
			Model:  model,
			Choices: []openAIStreamChoice{
				{
					Index: 0,
					Delta: openAIDelta{
						Role:    chunk.Delta.Role,
						Content: chunk.Delta.Content,
					},
					FinishReason: nilIfEmpty(chunk.FinishReason),
				},
			},
		}

		data, err := json.Marshal(sseChunk)
		if err != nil {
			continue
		}

		_, _ = fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	// Send [DONE] sentinel
	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// OpenAI streaming types

type openAIStreamChunk struct {
	ID      string               `json:"id"`
	Object  string               `json:"object"`
	Model   string               `json:"model"`
	Choices []openAIStreamChoice `json:"choices"`
}

type openAIStreamChoice struct {
	Index        int         `json:"index"`
	Delta        openAIDelta `json:"delta"`
	FinishReason *string     `json:"finish_reason"`
}

type openAIDelta struct {
	Role    string `json:"role,omitempty"`
	Content string `json:"content,omitempty"`
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
