package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/xraph/nexus/provider"
)

func (a *API) handleCreateEmbedding(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	// Parse request — accept string or array for "input"
	var raw map[string]json.RawMessage
	if unmarshalErr := json.Unmarshal(body, &raw); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var req provider.EmbeddingRequest
	if modelRaw, ok := raw["model"]; ok {
		_ = json.Unmarshal(modelRaw, &req.Model) //nolint:errcheck // best-effort; validated below
	}

	if inputRaw, ok := raw["input"]; ok {
		var single string
		if unmarshalErr := json.Unmarshal(inputRaw, &single); unmarshalErr == nil {
			req.Input = []string{single}
		} else {
			_ = json.Unmarshal(inputRaw, &req.Input) //nolint:errcheck // best-effort; validated below
		}
	}

	if req.Model == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	resp, err := a.gw.Engine().Embed(r.Context(), &req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, resp)
}
