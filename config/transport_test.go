package config

import (
	"net/http"
	"testing"
)

// transportWrapper is provider.TransportWrapper, spelled out so this module
// does not need a root version that declares it.
type transportWrapper interface {
	WrapTransport(wrap func(http.RoundTripper) http.RoundTripper)
}

// Every provider config can build lets the gateway wrap its HTTP transport,
// so the Forge extension traces every outbound call.
func TestEveryBuiltProviderLetsTheGatewayWrapItsTransport(t *testing.T) {
	types := []string{
		"openai", "anthropic", "opencompat", "groq", "together", "mistral", "deepseek", "xai",
		"openrouter", "ollama", "lmstudio", "fireworks", "perplexity", "cerebras", "sambanova",
		"deepinfra", "lepton", "novita", "nvidia", "anyscale", "hyperbolic", "nebius", "gemini",
		"cohere", "ai21", "bedrock", "azureopenai", "vertex", "voyageai", "jinaai",
	}
	if len(types) != 30 {
		t.Fatalf("listed %d provider types, want the 30 released ones", len(types))
	}
	for _, typ := range types {
		p := buildProvider(ProviderConfig{Type: typ, Name: typ, APIKey: "k", BaseURL: "http://127.0.0.1:1"})
		if p == nil {
			t.Errorf("%s: buildProvider returned nil", typ)
			continue
		}
		if _, ok := p.(transportWrapper); !ok {
			t.Errorf("%s (%T) does not implement WrapTransport", typ, p)
		}
	}
}
