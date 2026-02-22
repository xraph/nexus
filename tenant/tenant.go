// Package tenant defines the multi-tenant management types and service.
package tenant

import (
	"context"
	"time"

	"github.com/xraph/nexus/id"
)

// Tenant represents an isolated customer / team / project.
type Tenant struct {
	ID        id.TenantID       `json:"id"`
	Name      string            `json:"name"`
	Slug      string            `json:"slug"` // URL-safe identifier
	Status    Status            `json:"status"`
	Quota     Quota             `json:"quota"`
	Config    TenantConfig      `json:"config"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

// Status represents the tenant's current state.
type Status string

const (
	StatusActive    Status = "active"
	StatusDisabled  Status = "disabled"
	StatusSuspended Status = "suspended"
)

// Quota defines limits for a tenant.
type Quota struct {
	RPM              int     `json:"rpm"`                // requests per minute
	TPM              int     `json:"tpm"`                // tokens per minute
	DailyRequests    int     `json:"daily_requests"`     // max requests per day (0 = unlimited)
	MonthlyBudgetUSD float64 `json:"monthly_budget_usd"` // max spend per month (0 = unlimited)
	MaxTokensPerReq  int     `json:"max_tokens_per_req"` // max tokens per single request
}

// TenantConfig holds per-tenant overrides.
type TenantConfig struct {
	AllowedModels   []string          `json:"allowed_models,omitempty"`
	BlockedModels   []string          `json:"blocked_models,omitempty"`
	DefaultModel    string            `json:"default_model,omitempty"`
	RoutingStrategy string            `json:"routing_strategy,omitempty"`
	GuardrailPolicy string            `json:"guardrail_policy,omitempty"`
	CacheEnabled    *bool             `json:"cache_enabled,omitempty"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

// CreateInput is the input for creating a tenant.
type CreateInput struct {
	Name     string            `json:"name"`
	Slug     string            `json:"slug"`
	Quota    *Quota            `json:"quota,omitempty"`
	Config   *TenantConfig     `json:"config,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// UpdateInput is the input for updating a tenant.
type UpdateInput struct {
	Name     *string           `json:"name,omitempty"`
	Quota    *Quota            `json:"quota,omitempty"`
	Config   *TenantConfig     `json:"config,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// ListOptions configures tenant listing.
type ListOptions struct {
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// Service manages tenant lifecycle.
type Service interface {
	Create(ctx context.Context, input *CreateInput) (*Tenant, error)
	Get(ctx context.Context, id string) (*Tenant, error)
	GetBySlug(ctx context.Context, slug string) (*Tenant, error)
	Update(ctx context.Context, id string, input *UpdateInput) (*Tenant, error)
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, opts *ListOptions) ([]*Tenant, int, error)
	UpdateQuota(ctx context.Context, id string, quota *Quota) error
	SetStatus(ctx context.Context, id string, status Status) error
}

// Store is the persistence interface for tenants.
type Store interface {
	Insert(ctx context.Context, t *Tenant) error
	FindByID(ctx context.Context, id string) (*Tenant, error)
	FindBySlug(ctx context.Context, slug string) (*Tenant, error)
	Update(ctx context.Context, t *Tenant) error
	Delete(ctx context.Context, id string) error
	List(ctx context.Context, opts *ListOptions) ([]*Tenant, int, error)
}
