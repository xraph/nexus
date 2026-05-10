// Example: OpenAI-compatible proxy server.
//
// This example wires the Nexus proxy with the full streaming surface:
//
//   - SSE (default, OpenAI-compatible) at POST /v1/chat/completions
//   - NDJSON via Accept: application/x-ndjson or ?stream_format=ndjson
//   - Native nexus SSE via Accept: application/vnd.nexus.events+sse
//   - WebSocket bidirectional streaming at /v1/realtime
//   - Stream record-and-replay caching (in-memory)
//   - Plugin lifecycle metrics for streamed traffic
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./_examples/proxy
//
// Then from Python:
//
//	from openai import OpenAI
//	client = OpenAI(base_url="http://localhost:8080/v1", api_key="unused")
//	resp = client.chat.completions.create(
//	    model="gpt-4o-mini",
//	    messages=[{"role": "user", "content": "Hello!"}],
//	    stream=True,
//	)
//	for chunk in resp:
//	    print(chunk.choices[0].delta.content or "", end="", flush=True)
package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/cache"
	"github.com/xraph/nexus/cache/stores"
	"github.com/xraph/nexus/observability"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/proxy"
	"github.com/xraph/nexus/router/strategies"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	// MetricsExtension implements all the streaming hooks added in the
	// streaming work — counts streams started/completed/failed and bytes
	// emitted, exposed via its public Counter fields.
	metrics := observability.NewMetricsExtension()

	// Engine with provider + extension + stream cache.
	engine := nexus.NewEngine(
		nexus.WithProvider(openai.New(apiKey)),
		nexus.WithRouter(strategies.NewPriority()),
		nexus.WithExtension(metrics),
		nexus.WithStreamCache(stores.NewMemoryStream(), cache.StreamCacheOptions{
			TTL:  10 * time.Minute,
			Mode: cache.ReplayBurst,
		}),
	)

	// Proxy: SSE / NDJSON / native-SSE / WebSocket all wired by default.
	p := proxy.New(engine)

	srv := &http.Server{
		Addr:              ":8080",
		Handler:           p,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Background goroutine to print metrics every 10s so you can watch
	// streamed traffic light up the counters as you make requests.
	go func() {
		t := time.NewTicker(10 * time.Second)
		defer t.Stop()
		for range t.C {
			fmt.Printf("streams: started=%d completed=%d failed=%d chunks=%d bytes=%d\n",
				metrics.StreamsStarted.Value(),
				metrics.StreamsCompleted.Value(),
				metrics.StreamsFailed.Value(),
				metrics.ChunksEmitted.Value(),
				metrics.ChunkBytes.Value(),
			)
		}
	}()

	// Graceful shutdown — signals tear in-flight streams via Proxy.Shutdown,
	// then close the listener with http.Server.Shutdown.
	idle := make(chan struct{})
	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh

		fmt.Println("shutting down — canceling active streams")
		_ = p.Shutdown(context.Background())

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		close(idle)
	}()

	fmt.Println("Nexus proxy listening on :8080")
	fmt.Println("- SSE         POST /v1/chat/completions  (default)")
	fmt.Println("- NDJSON      POST /v1/chat/completions  (Accept: application/x-ndjson)")
	fmt.Println("- Native SSE  POST /v1/chat/completions  (Accept: application/vnd.nexus.events+sse)")
	fmt.Println("- WebSocket   GET  /v1/realtime")

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
	<-idle
}
