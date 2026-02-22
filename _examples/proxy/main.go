// Example: OpenAI-compatible proxy server.
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./_examples/proxy
//
// Then from Python:
//
//	from openai import OpenAI
//	client = OpenAI(base_url="http://localhost:8080/v1", api_key="unused")
//	resp = client.chat.completions.create(model="gpt-4o-mini", messages=[{"role": "user", "content": "Hello!"}])
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/proxy"
	"github.com/xraph/nexus/router/strategies"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	// Create a Nexus engine with OpenAI provider
	engine := nexus.NewEngine(
		nexus.WithProvider(openai.New(apiKey)),
		nexus.WithRouter(strategies.NewPriority()),
	)

	// Create the OpenAI-compatible proxy
	p := proxy.New(engine)

	addr := ":8080"
	fmt.Printf("Nexus proxy listening on %s\n", addr)
	fmt.Println("Point any OpenAI SDK at http://localhost:8080/v1")
	log.Fatal(http.ListenAndServe(addr, p))
}
