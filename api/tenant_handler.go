package api

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/xraph/nexus/tenant"
)

func (a *API) handleCreateTenant(w http.ResponseWriter, r *http.Request) {
	if a.gw.Tenants() == nil {
		writeError(w, http.StatusNotImplemented, "tenant service not configured")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input tenant.CreateInput
	if unmarshalErr := json.Unmarshal(body, &input); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+unmarshalErr.Error())
		return
	}

	t, err := a.gw.Tenants().Create(r.Context(), &input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, t)
}

func (a *API) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if a.gw.Tenants() == nil {
		writeError(w, http.StatusNotImplemented, "tenant service not configured")
		return
	}

	tenants, total, err := a.gw.Tenants().List(r.Context(), nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":  tenants,
		"total": total,
	})
}

func (a *API) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	if a.gw.Tenants() == nil {
		writeError(w, http.StatusNotImplemented, "tenant service not configured")
		return
	}

	id := r.PathValue("id")
	t, err := a.gw.Tenants().Get(r.Context(), id)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (a *API) handleUpdateTenant(w http.ResponseWriter, r *http.Request) {
	if a.gw.Tenants() == nil {
		writeError(w, http.StatusNotImplemented, "tenant service not configured")
		return
	}

	id := r.PathValue("id")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read request body")
		return
	}
	defer func() { _ = r.Body.Close() }()

	var input tenant.UpdateInput
	if unmarshalErr := json.Unmarshal(body, &input); unmarshalErr != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON: "+unmarshalErr.Error())
		return
	}

	t, err := a.gw.Tenants().Update(r.Context(), id, &input)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, t)
}

func (a *API) handleDeleteTenant(w http.ResponseWriter, r *http.Request) {
	if a.gw.Tenants() == nil {
		writeError(w, http.StatusNotImplemented, "tenant service not configured")
		return
	}

	id := r.PathValue("id")
	if err := a.gw.Tenants().Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
