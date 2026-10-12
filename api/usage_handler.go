package api

import (
	"errors"
	"net/http"

	"github.com/xraph/nexus/usage"
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
		if errors.Is(err, usage.ErrInvalidPeriod) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err != nil {
			a.writeAdminError(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, summary)
		return
	}

	writeError(w, http.StatusBadRequest, "tenant_id query parameter is required")
}
