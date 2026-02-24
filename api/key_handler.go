package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/xraph/nexus/key"
)

func (a *API) handleCreateKey(w http.ResponseWriter, r *http.Request) {
	if a.gw.Keys() == nil {
		writeError(w, http.StatusNotImplemented, "key service not configured")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input key.CreateInput
	if unmarshalErr := json.Unmarshal(body, &input); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+unmarshalErr.Error())
		return
	}

	k, rawKey, err := a.gw.Keys().Create(r.Context(), &input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Return key info + raw key (only visible this one time)
	writeJSON(w, http.StatusCreated, map[string]any{
		"key":     k,
		"api_key": rawKey,
	})
}

func (a *API) handleListKeys(w http.ResponseWriter, r *http.Request) {
	if a.gw.Keys() == nil {
		writeError(w, http.StatusNotImplemented, "key service not configured")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	if tenantID == "" {
		writeError(w, http.StatusBadRequest, "tenant_id query parameter is required")
		return
	}

	keys, err := a.gw.Keys().List(r.Context(), tenantID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": keys,
	})
}

func (a *API) handleRevokeKey(w http.ResponseWriter, r *http.Request) {
	if a.gw.Keys() == nil {
		writeError(w, http.StatusNotImplemented, "key service not configured")
		return
	}

	id := r.PathValue("id")
	if err := a.gw.Keys().Revoke(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
