package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/xraph/nexus/provider"
)

func (a *API) handleCreateCompletion(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var req provider.CompletionRequest
	if unmarshalErr := json.Unmarshal(body, &req); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+unmarshalErr.Error())
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	ctx := r.Context()

	// Streaming
	if req.Stream {
		a.handleStreamCompletion(ctx, w, &req)
		return
	}

	resp, err := a.gw.Engine().Complete(ctx, &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleStreamCompletion(ctx context.Context, w http.ResponseWriter, req *provider.CompletionRequest) {
	stream, err := a.gw.Engine().CompleteStream(ctx, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer func() { _ = stream.Close() }()

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	for {
		chunk, streamErr := stream.Next(ctx)
		if errors.Is(streamErr, io.EOF) {
			break
		}
		if streamErr != nil {
			break
		}

		data, marshalErr := json.Marshal(chunk)
		if marshalErr != nil {
			break
		}
		_, _ = fmt.Fprintf(w, "data: %s\n\n", data) //nolint:gosec // G705 -- SSE stream, not HTML
		flusher.Flush()
	}

	_, _ = fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
