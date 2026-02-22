package api

import (
	"context"
	"encoding/json"
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
	defer r.Body.Close()

	var req provider.CompletionRequest
	if err := json.Unmarshal(body, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	ctx := r.Context()

	// Streaming
	if req.Stream {
		a.handleStreamCompletion(w, ctx, &req)
		return
	}

	resp, err := a.gw.Engine().Complete(ctx, &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleStreamCompletion(w http.ResponseWriter, ctx context.Context, req *provider.CompletionRequest) {
	stream, err := a.gw.Engine().CompleteStream(ctx, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer stream.Close()

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
		chunk, err := stream.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			break
		}

		data, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\n", data)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
