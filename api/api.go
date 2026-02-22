// Package api provides HTTP handlers for the Nexus gateway.
// These handlers use standard net/http and can be mounted on any
// HTTP router (chi, gorilla/mux, stdlib ServeMux, forge.Router).
//
// When used with forge, the extension package wraps these with
// forge.Context and OpenAPI metadata.
package api

import (
	"net/http"

	nexus "github.com/xraph/nexus"
)

// API wires all HTTP handlers for the Nexus gateway.
type API struct {
	gw  *nexus.Gateway
	mux *http.ServeMux
}

// New creates a new API handler set.
func New(gw *nexus.Gateway) *API {
	a := &API{
		gw:  gw,
		mux: http.NewServeMux(),
	}
	a.registerRoutes()
	return a
}

// Handler returns the http.Handler for the API.
func (a *API) Handler() http.Handler {
	return a.mux
}

func (a *API) registerRoutes() {
	// Completion routes
	a.mux.HandleFunc("POST /v1/chat/completions", a.handleCreateCompletion)

	// Embedding routes
	a.mux.HandleFunc("POST /v1/embeddings", a.handleCreateEmbedding)

	// Model routes
	a.mux.HandleFunc("GET /v1/models", a.handleListModels)
	a.mux.HandleFunc("GET /v1/models/{model}", a.handleGetModel)

	// Admin: Tenant routes
	a.mux.HandleFunc("POST /admin/tenants", a.handleCreateTenant)
	a.mux.HandleFunc("GET /admin/tenants", a.handleListTenants)
	a.mux.HandleFunc("GET /admin/tenants/{id}", a.handleGetTenant)
	a.mux.HandleFunc("PATCH /admin/tenants/{id}", a.handleUpdateTenant)
	a.mux.HandleFunc("DELETE /admin/tenants/{id}", a.handleDeleteTenant)

	// Admin: Key routes
	a.mux.HandleFunc("POST /admin/keys", a.handleCreateKey)
	a.mux.HandleFunc("GET /admin/keys", a.handleListKeys)
	a.mux.HandleFunc("DELETE /admin/keys/{id}", a.handleRevokeKey)

	// Admin: Usage routes
	a.mux.HandleFunc("GET /admin/usage", a.handleGetUsage)

	// Admin: Provider routes
	a.mux.HandleFunc("GET /admin/providers", a.handleListProviders)

	// Health
	a.mux.HandleFunc("GET /health", a.handleHealth)
}
