package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/pipeline"
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
		a.handleStreamCompletion(ctx, w, r, &req)
		return
	}

	resp, err := a.gw.Engine().Complete(ctx, &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) handleStreamCompletion(_ context.Context, w http.ResponseWriter, r *http.Request, req *provider.CompletionRequest) {
	ctx, cancel := a.streamContext(r.Context())
	defer cancel()
	stream, err := a.gw.Engine().CompleteStream(ctx, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	encoder := httpstream.Negotiate(r, a.encoders)
	if encoder == nil {
		_ = stream.Close()
		writeError(w, http.StatusInternalServerError, "no stream encoder available")
		return
	}

	httpstream.Run(ctx, w, stream, encoder, httpstream.RunOptions{
		RequestID: pipeline.RequestID(ctx),
	})
}
