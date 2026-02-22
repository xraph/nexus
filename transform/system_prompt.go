package transform

import (
	"context"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// SystemPromptTransform prepends a system message to every request.
// Supports global prompts and per-tenant overrides.
type SystemPromptTransform struct {
	// GlobalPrompt is prepended to all requests.
	GlobalPrompt string

	// TenantPrompts maps tenant IDs to tenant-specific system prompts.
	// If set, this replaces the global prompt for that tenant.
	TenantPrompts map[string]string

	// Append if true appends to existing system messages instead of prepending.
	Append bool
}

// NewSystemPrompt creates a system prompt transform.
func NewSystemPrompt(prompt string) *SystemPromptTransform {
	return &SystemPromptTransform{
		GlobalPrompt:  prompt,
		TenantPrompts: make(map[string]string),
	}
}

func (t *SystemPromptTransform) Name() string { return "system_prompt" }
func (t *SystemPromptTransform) Phase() Phase { return PhaseInput }

func (t *SystemPromptTransform) TransformInput(ctx context.Context, req *provider.CompletionRequest) error {
	prompt := t.GlobalPrompt

	// Check for tenant-specific override
	tenantID := pipeline.TenantID(ctx)
	if tenantID != "" {
		if tp, ok := t.TenantPrompts[tenantID]; ok {
			prompt = tp
		}
	}

	if prompt == "" {
		return nil
	}

	systemMsg := provider.Message{
		Role:    "system",
		Content: prompt,
	}

	if t.Append {
		req.Messages = append(req.Messages, systemMsg)
	} else {
		// Prepend: put system message at the front
		req.Messages = append([]provider.Message{systemMsg}, req.Messages...)
	}

	return nil
}

// WithTenantPrompt adds a tenant-specific system prompt.
func (t *SystemPromptTransform) WithTenantPrompt(tenantID, prompt string) *SystemPromptTransform {
	t.TenantPrompts[tenantID] = prompt
	return t
}
