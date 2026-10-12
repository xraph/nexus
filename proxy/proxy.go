// Package proxy provides an OpenAI-compatible HTTP server.
// Point any OpenAI SDK client at this proxy and it works transparently —
// Nexus handles routing, caching, guardrails, and observability behind the scenes.
//
// Usage:
//
//	engine := nexus.NewEngine(
//	    nexus.WithProvider(openai.New("sk-...")),
//	    nexus.WithProvider(anthropic.New("sk-ant-...")),
//	)
//	p := proxy.New(engine)
//	http.ListenAndServe(":8080", p)
//
// Then from any OpenAI SDK:
//
//	client = OpenAI(base_url="http://localhost:8080/v1", api_key="nxs_...")
package proxy

import (
	"context"
	"net/http"
	"sync"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/key"
)

// Proxy is an OpenAI-compatible HTTP server.
// It translates OpenAI API requests into Nexus pipeline calls.
type Proxy struct {
	engine     *nexus.Engine
	mux        *http.ServeMux
	encoders   *httpstream.Registry
	wsOptions  httpstream.WSOptions
	wsDisabled bool

	// shutdown wires every streaming request's context to a single
	// gateway-scoped cancel signal, so Shutdown can tear active SSE/WS
	// connections instead of letting net/http.Server.Shutdown block on
	// long-lived streams.
	shutdownOnce sync.Once
	baseCtx      context.Context
	baseCancel   context.CancelFunc
}

// Option configures the proxy.
type Option func(*Proxy)

// WithStreamEncoder registers an additional stream encoder. Called once per
// content type or alias; the proxy will negotiate it via Accept header,
// `?stream_format=` query, or `X-Nexus-Stream-Format` request header.
func WithStreamEncoder(contentType string, enc httpstream.StreamEncoder) Option {
	return func(p *Proxy) {
		if p.encoders == nil {
			p.encoders = httpstream.DefaultRegistry()
		}
		p.encoders.Register(contentType, enc)
	}
}

// WithStreamEncoderAlias adds an alias name for a registered encoder.
func WithStreamEncoderAlias(alias, contentType string) Option {
	return func(p *Proxy) {
		if p.encoders == nil {
			p.encoders = httpstream.DefaultRegistry()
		}
		p.encoders.RegisterAlias(alias, contentType)
	}
}

// WithWebSocket configures the bidirectional WebSocket endpoint at
// /v1/realtime. Pass an empty WSOptions{} for defaults.
func WithWebSocket(opts httpstream.WSOptions) Option {
	return func(p *Proxy) { p.wsOptions = opts }
}

// WithoutWebSocket disables the /v1/realtime WebSocket endpoint.
func WithoutWebSocket() Option {
	return func(p *Proxy) { p.wsDisabled = true }
}

// New creates a new OpenAI-compatible proxy. It panics when the engine's
// gateway has no key service, which means Initialize has not run: the routes
// cannot be protected, and serving them open would be worse than not serving
// them.
func New(engine *nexus.Engine, opts ...Option) *Proxy {
	if engine.Gateway().Keys() == nil {
		panic("proxy: New needs an initialized gateway (call Initialize first): the gateway has no key service")
	}
	baseCtx, baseCancel := context.WithCancel(context.Background())
	p := &Proxy{
		engine:     engine,
		mux:        http.NewServeMux(),
		encoders:   httpstream.DefaultRegistry(),
		baseCtx:    baseCtx,
		baseCancel: baseCancel,
	}
	for _, opt := range opts {
		opt(p)
	}
	p.registerRoutes()
	return p
}

// Shutdown signals every in-flight streaming request to terminate. Use this
// in tandem with http.Server.Shutdown to ensure long-lived SSE/WebSocket
// connections actually drain rather than blocking the listener.
//
// After Shutdown is called, any new requests still receive normal handling
// because http.ServeMux remains live, but their contexts are pre-canceled,
// which the streaming runner detects and tears down promptly.
func (p *Proxy) Shutdown(_ context.Context) error {
	p.shutdownOnce.Do(func() {
		if p.baseCancel != nil {
			p.baseCancel()
		}
	})
	return nil
}

// streamContext returns a derived context that is canceled when EITHER the
// request context or the proxy's base context is canceled. The latter is
// the lever Shutdown pulls to abort in-flight streams.
func (p *Proxy) streamContext(reqCtx context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(reqCtx)
	if p.baseCtx == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(p.baseCtx, cancel)
	return ctx, func() {
		stop()
		cancel()
	}
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for browser-based clients
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, x-api-key")
	// A browser page can read only the response headers CORS exposes, and
	// X-Request-Id is how a client error is matched to its usage row.
	w.Header().Set("Access-Control-Expose-Headers", "X-Request-Id")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	p.mux.ServeHTTP(w, r)
}

func (p *Proxy) registerRoutes() {
	gw := p.engine.Gateway()
	required := gw.Config().RequireAPIKey
	// A key is required unless the gateway is configured open, and a key that
	// is presented is always checked. The default OnError writes the OpenAI
	// error shape, with Retry-After and WWW-Authenticate.
	keyAuth := auth.KeyAuth(auth.KeyAuthOptions{Keys: gw.Keys(), Tenants: gw.Tenants(), Required: required, OnError: p.onAuthError})
	// Every route gets a request id ahead of the key check, so a refusal at
	// the edge has one to log and to return in X-Request-Id.
	rid := auth.RequestID()
	ka := func(h http.Handler) http.Handler { return rid(keyAuth(h)) }
	models := func(h http.Handler) http.Handler { return h }
	if required {
		// RequireScope fails closed (an anonymous request is a 401), so it
		// is added only when keys are required: an open gateway lists its
		// models to anyone.
		models = auth.RequireScope(key.ScopeModels, p.onAuthError)
	}

	// Completions and embeddings: the pipeline checks the scope.
	p.mux.Handle("POST /v1/chat/completions", ka(http.HandlerFunc(p.handleChatCompletions)))
	p.mux.Handle("POST /v1/embeddings", ka(http.HandlerFunc(p.handleEmbeddings)))
	p.mux.Handle("GET /v1/models", ka(models(http.HandlerFunc(p.handleListModels))))
	p.mux.Handle("GET /v1/models/{model}", ka(models(http.HandlerFunc(p.handleGetModel))))
	p.mux.HandleFunc("GET /health", p.handleHealth)
	if !p.wsDisabled {
		wsOpts := p.wsOptions
		wsOpts.OnError = p.onStreamError
		ws := httpstream.NewWSHandler(p.engine, wsOpts)
		p.mux.Handle("/v1/realtime", ka(ws))
	}
}

func (p *Proxy) onAuthError(w http.ResponseWriter, r *http.Request, err error) {
	p.writePipelineError(w, r, err)
}
