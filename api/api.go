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
	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/key"
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

// New creates a new API handler set. It panics when gw has no key service,
// which means Initialize has not run: the routes cannot be protected, and
// serving them open would be worse than not serving them.
func New(gw *nexus.Gateway, opts ...Option) *API {
	if gw.Keys() == nil {
		panic("api: New needs an initialized gateway (call Initialize first): the gateway has no key service")
	}
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
	keys := a.gw.Keys()
	required := a.gw.Config().RequireAPIKey
	// /v1 routes: a key is required unless the gateway is configured open,
	// and a key that is presented is always checked.
	rid := auth.RequestID()
	v1Auth := auth.KeyAuth(auth.KeyAuthOptions{Keys: keys, Tenants: a.gw.Tenants(), Required: required, OnError: a.onAuthError})
	// Every route gets a request id ahead of the key check, so a refusal at
	// the edge has one to log and to return in X-Request-Id.
	v1 := func(h http.Handler) http.Handler { return rid(v1Auth(h)) }
	// /admin routes always need a key, whatever RequireAPIKey says: it opens
	// the /v1 routes only.
	adminKeys := auth.KeyAuth(auth.KeyAuthOptions{Keys: keys, Tenants: a.gw.Tenants(), Required: true, OnError: a.onAuthError})
	adminAuth := func(h http.Handler) http.Handler { return rid(adminKeys(h)) }
	admin := auth.RequireScope(key.ScopeAdmin, a.onAuthError)
	models := func(h http.Handler) http.Handler { return h }
	if required {
		// RequireScope fails closed (an anonymous request is a 401), so it
		// is added only when keys are required: an open gateway lists its
		// models to anyone.
		models = auth.RequireScope(key.ScopeModels, a.onAuthError)
	}

	// Completion and embedding routes: the pipeline checks the scope.
	a.mux.Handle("POST /v1/chat/completions", v1(http.HandlerFunc(a.handleCreateCompletion)))
	a.mux.Handle("POST /v1/embeddings", v1(http.HandlerFunc(a.handleCreateEmbedding)))

	// Model routes
	a.mux.Handle("GET /v1/models", v1(models(http.HandlerFunc(a.handleListModels))))
	a.mux.Handle("GET /v1/models/{model}", v1(models(http.HandlerFunc(a.handleGetModel))))

	adminRoute := func(pattern string, h http.HandlerFunc) {
		a.mux.Handle(pattern, adminAuth(admin(h)))
	}

	// Admin: Tenant routes
	adminRoute("POST /admin/tenants", a.handleCreateTenant)
	adminRoute("GET /admin/tenants", a.handleListTenants)
	adminRoute("GET /admin/tenants/{id}", a.handleGetTenant)
	adminRoute("PATCH /admin/tenants/{id}", a.handleUpdateTenant)
	adminRoute("DELETE /admin/tenants/{id}", a.handleDeleteTenant)

	// Admin: Key routes
	adminRoute("POST /admin/keys", a.handleCreateKey)
	adminRoute("GET /admin/keys", a.handleListKeys)
	adminRoute("DELETE /admin/keys/{id}", a.handleRevokeKey)

	// Admin: Usage routes
	adminRoute("GET /admin/usage", a.handleGetUsage)

	// Admin: Provider routes
	adminRoute("GET /admin/providers", a.handleListProviders)

	// Health stays open.
	a.mux.HandleFunc("GET /health", a.handleHealth)

	// Bidirectional WebSocket, opt-out via WithoutWebSocket. The upgrade is
	// authenticated like any /v1 route; the pipeline checks scopes per
	// request.
	if !a.wsDisabled {
		wsOpts := a.wsOptions
		wsOpts.OnError = a.onStreamError
		ws := httpstream.NewWSHandler(a.gw.Engine(), wsOpts)
		a.mux.Handle("/v1/realtime", v1(ws))
	}
}
