module github.com/xraph/nexus/config

go 1.25

require (
	github.com/xraph/nexus v0.0.0
	github.com/xraph/nexus/providers/ai21 v0.0.0
	github.com/xraph/nexus/providers/anthropic v0.0.0
	github.com/xraph/nexus/providers/anyscale v0.0.0
	github.com/xraph/nexus/providers/azureopenai v0.0.0
	github.com/xraph/nexus/providers/bedrock v0.0.0
	github.com/xraph/nexus/providers/cerebras v0.0.0
	github.com/xraph/nexus/providers/cohere v0.0.0
	github.com/xraph/nexus/providers/deepinfra v0.0.0
	github.com/xraph/nexus/providers/deepseek v0.0.0
	github.com/xraph/nexus/providers/fireworks v0.0.0
	github.com/xraph/nexus/providers/gemini v0.0.0
	github.com/xraph/nexus/providers/groq v0.0.0
	github.com/xraph/nexus/providers/hyperbolic v0.0.0
	github.com/xraph/nexus/providers/jinaai v0.0.0
	github.com/xraph/nexus/providers/lepton v0.0.0
	github.com/xraph/nexus/providers/lmstudio v0.0.0
	github.com/xraph/nexus/providers/mistral v0.0.0
	github.com/xraph/nexus/providers/nebius v0.0.0
	github.com/xraph/nexus/providers/novita v0.0.0
	github.com/xraph/nexus/providers/nvidia v0.0.0
	github.com/xraph/nexus/providers/ollama v0.0.0
	github.com/xraph/nexus/providers/openai v0.0.0
	github.com/xraph/nexus/providers/opencompat v0.0.0
	github.com/xraph/nexus/providers/openrouter v0.0.0
	github.com/xraph/nexus/providers/perplexity v0.0.0
	github.com/xraph/nexus/providers/sambanova v0.0.0
	github.com/xraph/nexus/providers/together v0.0.0
	github.com/xraph/nexus/providers/vertex v0.0.0
	github.com/xraph/nexus/providers/voyageai v0.0.0
	github.com/xraph/nexus/providers/xai v0.0.0
)

require (
	github.com/gofrs/uuid/v5 v5.2.0 // indirect
	go.jetify.com/typeid v1.3.0 // indirect
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
