package api

import (
	"net/http"
	"time"
)

func (a *API) handleListModels(w http.ResponseWriter, r *http.Request) {
	models, err := a.gw.Engine().ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	type modelEntry struct {
		ID      string `json:"id"`
		Object  string `json:"object"`
		Created int64  `json:"created"`
		OwnedBy string `json:"owned_by"`
	}

	var data []modelEntry
	for _, m := range models {
		data = append(data, modelEntry{
			ID:      m.Name,
			Object:  "model",
			Created: time.Now().Unix(),
			OwnedBy: m.Provider,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"object": "list",
		"data":   data,
	})
}

func (a *API) handleGetModel(w http.ResponseWriter, r *http.Request) {
	modelID := r.PathValue("model")
	if modelID == "" {
		writeError(w, http.StatusBadRequest, "model is required")
		return
	}

	models, err := a.gw.Engine().ListModels(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	for _, m := range models {
		if m.Name == modelID {
			writeJSON(w, http.StatusOK, map[string]any{
				"id":       m.Name,
				"object":   "model",
				"created":  time.Now().Unix(),
				"owned_by": m.Provider,
			})
			return
		}
	}

	writeError(w, http.StatusNotFound, "model not found: "+modelID)
}
