package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/xraph/nexus/paging"
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
		a.writeAdminError(w, r, err)
		return
	}

	writeJSON(w, http.StatusCreated, t)
}

func (a *API) handleListTenants(w http.ResponseWriter, r *http.Request) {
	if a.gw.Tenants() == nil {
		writeError(w, http.StatusNotImplemented, "tenant service not configured")
		return
	}

	res, err := a.gw.Tenants().List(r.Context(), &tenant.ListOptions{
		Status: r.URL.Query().Get("status"),
		Search: r.URL.Query().Get("search"),
		Cursor: r.URL.Query().Get("cursor"),
	})
	if errors.Is(err, paging.ErrInvalidCursor) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		a.writeAdminError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data":        res.Items,
		"next_cursor": res.NextCursor,
	})
}

func (a *API) handleGetTenant(w http.ResponseWriter, r *http.Request) {
	if a.gw.Tenants() == nil {
		writeError(w, http.StatusNotImplemented, "tenant service not configured")
		return
	}

	id := r.PathValue("id")
	t, err := a.gw.Tenants().Get(r.Context(), id)
	if errors.Is(err, tenant.ErrNotFound) {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if err != nil {
		a.writeAdminError(w, r, err)
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
	if errors.Is(err, tenant.ErrNotFound) {
		writeError(w, http.StatusNotFound, "tenant not found")
		return
	}
	if err != nil {
		a.writeAdminError(w, r, err)
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
		a.writeAdminError(w, r, err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
