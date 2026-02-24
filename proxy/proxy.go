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
	"net/http"

	nexus "github.com/xraph/nexus"
)

// Proxy is an OpenAI-compatible HTTP server.
// It translates OpenAI API requests into Nexus pipeline calls.
type Proxy struct {
	engine *nexus.Engine
	mux    *http.ServeMux
}

// Option configures the proxy.
type Option func(*Proxy)

// New creates a new OpenAI-compatible proxy.
func New(engine *nexus.Engine, opts ...Option) *Proxy {
	p := &Proxy{
		engine: engine,
		mux:    http.NewServeMux(),
	}
	for _, opt := range opts {
		opt(p)
	}
	p.registerRoutes()
	return p
}

// ServeHTTP implements http.Handler.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Add CORS headers for browser-based clients
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	p.mux.ServeHTTP(w, r)
}

func (p *Proxy) registerRoutes() {
	p.mux.HandleFunc("POST /v1/chat/completions", p.handleChatCompletions)
	p.mux.HandleFunc("POST /v1/embeddings", p.handleEmbeddings)
	p.mux.HandleFunc("GET /v1/models", p.handleListModels)
	p.mux.HandleFunc("GET /v1/models/{model}", p.handleGetModel)
	p.mux.HandleFunc("GET /health", p.handleHealth)
}
