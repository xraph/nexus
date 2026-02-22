package api

import (
	"net/http"
)

func (a *API) handleGetUsage(w http.ResponseWriter, r *http.Request) {
	if a.gw.Usage() == nil {
		writeError(w, http.StatusNotImplemented, "usage service not configured")
		return
	}

	tenantID := r.URL.Query().Get("tenant_id")
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "month"
	}

	if tenantID != "" {
		summary, err := a.gw.Usage().Summary(r.Context(), tenantID, period)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}

	writeError(w, http.StatusBadRequest, "tenant_id query parameter is required")
}
