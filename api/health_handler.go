package api

import (
	"net/http"
)

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":    "ok",
		"server":    "nexus",
		"providers": a.gw.Providers().Count(),
	})
}
