package testutil

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// WriteSSEChunks writes SSE data lines to the ResponseWriter, then flushes.
func WriteSSEChunks(w http.ResponseWriter, chunks []string) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for _, chunk := range chunks {
		_, _ = fmt.Fprintf(w, "data: %s\n\n", chunk)
		flusher.Flush()
	}
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

// OpenAIChunkJSON returns a valid OpenAI-format streaming chunk JSON string.
func OpenAIChunkJSON(id, model, content, finishReason string) string {
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   model,
		"choices": []map[string]any{
			{
				"index": 0,
				"delta": map[string]any{},
			},
		},
	}

	delta := chunk["choices"].([]map[string]any)[0]["delta"].(map[string]any)
	if content != "" {
		delta["content"] = content
	}
	if finishReason != "" {
		chunk["choices"].([]map[string]any)[0]["finish_reason"] = finishReason
	} else {
		chunk["choices"].([]map[string]any)[0]["finish_reason"] = nil
	}

	b, _ := json.Marshal(chunk)
	return string(b)
}

// OpenAIChunkJSONWithUsage returns a final OpenAI chunk with usage info.
func OpenAIChunkJSONWithUsage(id, model string, promptTokens, completionTokens, totalTokens int) string {
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": 1700000000,
		"model":   model,
		"choices": []map[string]any{},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      totalTokens,
		},
	}
	b, _ := json.Marshal(chunk)
	return string(b)
}

// AnthropicEventJSON returns a valid Anthropic SSE event pair (event: + data:).
func AnthropicEventJSON(eventType string, data any) string {
	b, _ := json.Marshal(data)
	return fmt.Sprintf("event: %s\ndata: %s", eventType, string(b))
}

// OpenAIStreamHandler returns an http.HandlerFunc that streams OpenAI-format chunks.
func OpenAIStreamHandler(chunks []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		WriteSSEChunks(w, chunks)
	}
}

// AnthropicStreamHandler returns an http.HandlerFunc that writes raw Anthropic SSE lines.
func AnthropicStreamHandler(lines []string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "streaming not supported", http.StatusInternalServerError)
			return
		}

		for _, line := range lines {
			_, _ = fmt.Fprintf(w, "%s\n", line)
			flusher.Flush()
		}
		// Final newline to end
		_, _ = fmt.Fprint(w, "\n")
		flusher.Flush()
	}
}
