// Package api provides HTTP handlers for the Nexus gateway.
// These handlers use standard net/http and can be mounted on any
// HTTP router (chi, gorilla/mux, stdlib ServeMux, forge.Router).
//
// When used with forge, the extension package wraps these with
// forge.Context and OpenAPI metadata.
package api

import (
	"context"
	"net/http"
	"sync"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/httpstream"
)

// API wires all HTTP handlers for the Nexus gateway.
type API struct {
	gw         *nexus.Gateway
	mux        *http.ServeMux
	encoders   *httpstream.Registry
	wsOptions  httpstream.WSOptions
	wsDisabled bool

	// shutdownOnce + baseCtx mirror the proxy package's pattern: every
	// streaming request derives its ctx from baseCtx via streamContext, so
	// API.Shutdown can preempt long-lived SSE/WebSocket connections.
	shutdownOnce sync.Once
	baseCtx      context.Context
	baseCancel   context.CancelFunc
}

// Option configures the API handler set.
type Option func(*API)

// WithStreamEncoder registers a stream encoder for content-type negotiation
// on streaming completion responses.
func WithStreamEncoder(contentType string, enc httpstream.StreamEncoder) Option {
	return func(a *API) {
		if a.encoders == nil {
			a.encoders = httpstream.DefaultRegistry()
		}
		a.encoders.Register(contentType, enc)
	}
}

// WithWebSocket configures the bidirectional WebSocket endpoint at
// /v1/realtime. Pass an empty WSOptions{} for defaults.
func WithWebSocket(opts httpstream.WSOptions) Option {
	return func(a *API) { a.wsOptions = opts }
}

// WithoutWebSocket disables the /v1/realtime WebSocket endpoint.
func WithoutWebSocket() Option {
	return func(a *API) { a.wsDisabled = true }
}

// New creates a new API handler set.
func New(gw *nexus.Gateway, opts ...Option) *API {
	baseCtx, baseCancel := context.WithCancel(context.Background())
	a := &API{
		gw:         gw,
		mux:        http.NewServeMux(),
		encoders:   httpstream.DefaultRegistry(),
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
	}
	for _, opt := range opts {
		opt(a)
	}
	a.registerRoutes()
	return a
}

// Handler returns the http.Handler for the API.
func (a *API) Handler() http.Handler {
	return a.mux
}

// Shutdown cancels every in-flight streaming request. Use in tandem with
// http.Server.Shutdown so long-lived SSE/WebSocket connections drain
// rather than blocking the listener.
func (a *API) Shutdown(_ context.Context) error {
	a.shutdownOnce.Do(func() {
		if a.baseCancel != nil {
			a.baseCancel()
		}
	})
	return nil
}

// streamContext returns a derived context that cancels when EITHER the
// request context or the API's base context cancels. The latter is the
// lever Shutdown pulls.
func (a *API) streamContext(reqCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(reqCtx)
	if a.baseCtx == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(a.baseCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
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

	// Bidirectional WebSocket — opt-out via WithoutWebSocket.
	if !a.wsDisabled {
		ws := httpstream.NewWSHandler(a.gw.Engine(), a.wsOptions)
		a.mux.Handle("/v1/realtime", ws)
	}
}
