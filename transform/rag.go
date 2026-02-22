package transform

import (
	"context"
	"fmt"
	"strings"

	"github.com/xraph/nexus/provider"
)

// RAGProvider retrieves context for a query.
type RAGProvider interface {
	// Retrieve returns relevant context chunks for the query.
	Retrieve(ctx context.Context, query string, maxResults int) ([]RAGChunk, error)
}

// RAGChunk is a piece of retrieved context.
type RAGChunk struct {
	Content  string  // The text content
	Source   string  // Where it came from
	Score    float64 // Relevance score (0-1)
	Metadata map[string]string
}

// RAGTransform injects retrieved context into requests.
type RAGTransform struct {
	provider   RAGProvider
	maxResults int
	template   string
}

// NewRAG creates a RAG context injection transform.
func NewRAG(rag RAGProvider) *RAGTransform {
	return &RAGTransform{
		provider:   rag,
		maxResults: 5,
		template:   defaultRAGTemplate,
	}
}

const defaultRAGTemplate = `Here is relevant context to help answer the user's question:

%s

Please use this context to inform your response. If the context doesn't contain relevant information, use your general knowledge.`

func (t *RAGTransform) Name() string { return "rag" }
func (t *RAGTransform) Phase() Phase { return PhaseInput }

func (t *RAGTransform) TransformInput(ctx context.Context, req *provider.CompletionRequest) error {
	// Extract query from the last user message
	query := extractLastUserMessage(req.Messages)
	if query == "" {
		return nil
	}

	chunks, err := t.provider.Retrieve(ctx, query, t.maxResults)
	if err != nil {
		// RAG failures are non-fatal — continue without context
		return nil
	}

	if len(chunks) == 0 {
		return nil
	}

	// Build context string
	var parts []string
	for i, chunk := range chunks {
		part := fmt.Sprintf("[%d] %s", i+1, chunk.Content)
		if chunk.Source != "" {
			part += fmt.Sprintf(" (source: %s)", chunk.Source)
		}
		parts = append(parts, part)
	}
	contextStr := strings.Join(parts, "\n\n")

	// Inject as a system message
	ragMsg := provider.Message{
		Role:    "system",
		Content: fmt.Sprintf(t.template, contextStr),
	}

	// Insert after any existing system messages, before user messages
	insertIdx := 0
	for i, msg := range req.Messages {
		if msg.Role == "system" {
			insertIdx = i + 1
		} else {
			break
		}
	}

	// Insert at position
	req.Messages = append(req.Messages[:insertIdx], append([]provider.Message{ragMsg}, req.Messages[insertIdx:]...)...)

	return nil
}

// WithMaxResults sets the maximum number of RAG results.
func (t *RAGTransform) WithMaxResults(n int) *RAGTransform {
	t.maxResults = n
	return t
}

// WithTemplate sets a custom template for the RAG context injection.
// The template should contain a single %s placeholder for the context.
func (t *RAGTransform) WithTemplate(tmpl string) *RAGTransform {
	t.template = tmpl
	return t
}

func extractLastUserMessage(messages []provider.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			if s, ok := messages[i].Content.(string); ok {
				return s
			}
		}
	}
	return ""
}
