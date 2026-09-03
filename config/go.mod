module github.com/xraph/nexus/config

go 1.26.0

require (
	github.com/xraph/nexus v1.6.2
	github.com/xraph/nexus/providers/ai21 v1.6.2
	github.com/xraph/nexus/providers/anthropic v1.6.2
	github.com/xraph/nexus/providers/anyscale v1.6.2
	github.com/xraph/nexus/providers/azureopenai v1.6.2
	github.com/xraph/nexus/providers/bedrock v1.6.2
	github.com/xraph/nexus/providers/cerebras v1.6.2
	github.com/xraph/nexus/providers/cohere v1.6.2
	github.com/xraph/nexus/providers/deepinfra v1.6.2
	github.com/xraph/nexus/providers/deepseek v1.6.2
	github.com/xraph/nexus/providers/fireworks v1.6.2
	github.com/xraph/nexus/providers/gemini v1.6.2
	github.com/xraph/nexus/providers/groq v1.6.2
	github.com/xraph/nexus/providers/hyperbolic v1.6.2
	github.com/xraph/nexus/providers/jinaai v1.6.2
	github.com/xraph/nexus/providers/lepton v1.6.2
	github.com/xraph/nexus/providers/lmstudio v1.6.2
	github.com/xraph/nexus/providers/mistral v1.6.2
	github.com/xraph/nexus/providers/nebius v1.6.2
	github.com/xraph/nexus/providers/novita v1.6.2
	github.com/xraph/nexus/providers/nvidia v1.6.2
	github.com/xraph/nexus/providers/ollama v1.6.2
	github.com/xraph/nexus/providers/openai v1.6.2
	github.com/xraph/nexus/providers/opencompat v1.6.2
	github.com/xraph/nexus/providers/openrouter v1.6.2
	github.com/xraph/nexus/providers/perplexity v1.6.2
	github.com/xraph/nexus/providers/sambanova v1.6.2
	github.com/xraph/nexus/providers/together v1.6.2
	github.com/xraph/nexus/providers/vertex v1.6.2
	github.com/xraph/nexus/providers/voyageai v1.6.2
	github.com/xraph/nexus/providers/xai v1.6.2
)

require (
	github.com/gofrs/uuid/v5 v5.3.2 // indirect
	github.com/xraph/go-utils v1.2.2 // indirect
	go.jetify.com/typeid/v2 v2.0.0-alpha.3 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.28.0 // indirect
)

replace (
	github.com/xraph/nexus => ..
	github.com/xraph/nexus/providers/ai21 => ../providers/ai21
	github.com/xraph/nexus/providers/anthropic => ../providers/anthropic
	github.com/xraph/nexus/providers/anyscale => ../providers/anyscale
	github.com/xraph/nexus/providers/azureopenai => ../providers/azureopenai
	github.com/xraph/nexus/providers/bedrock => ../providers/bedrock
	github.com/xraph/nexus/providers/cerebras => ../providers/cerebras
	github.com/xraph/nexus/providers/cohere => ../providers/cohere
	github.com/xraph/nexus/providers/deepinfra => ../providers/deepinfra
	github.com/xraph/nexus/providers/deepseek => ../providers/deepseek
	github.com/xraph/nexus/providers/fireworks => ../providers/fireworks
	github.com/xraph/nexus/providers/gemini => ../providers/gemini
	github.com/xraph/nexus/providers/groq => ../providers/groq
	github.com/xraph/nexus/providers/hyperbolic => ../providers/hyperbolic
	github.com/xraph/nexus/providers/jinaai => ../providers/jinaai
	github.com/xraph/nexus/providers/lepton => ../providers/lepton
	github.com/xraph/nexus/providers/lmstudio => ../providers/lmstudio
	github.com/xraph/nexus/providers/mistral => ../providers/mistral
	github.com/xraph/nexus/providers/nebius => ../providers/nebius
	github.com/xraph/nexus/providers/novita => ../providers/novita
	github.com/xraph/nexus/providers/nvidia => ../providers/nvidia
	github.com/xraph/nexus/providers/ollama => ../providers/ollama
	github.com/xraph/nexus/providers/openai => ../providers/openai
	github.com/xraph/nexus/providers/opencompat => ../providers/opencompat
	github.com/xraph/nexus/providers/openrouter => ../providers/openrouter
	github.com/xraph/nexus/providers/perplexity => ../providers/perplexity
	github.com/xraph/nexus/providers/sambanova => ../providers/sambanova
	github.com/xraph/nexus/providers/together => ../providers/together
	github.com/xraph/nexus/providers/vertex => ../providers/vertex
	github.com/xraph/nexus/providers/voyageai => ../providers/voyageai
	github.com/xraph/nexus/providers/xai => ../providers/xai
)
