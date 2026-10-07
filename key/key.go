// Package key defines the API key management types and service.
package key

import (
	"context"
	"time"

	"github.com/xraph/nexus/id"
)

// APIKey represents a Nexus-issued API key.
type APIKey struct {
	ID         id.KeyID          `json:"id"`
	TenantID   id.TenantID       `json:"tenant_id"`
	Name       string            `json:"name"`
	Prefix     string            `json:"prefix"` // "nxs_" + first 8 chars (for display)
	Hash       string            `json:"-"`      // bcrypt hash (never exposed)
	Scopes     []string          `json:"scopes"` // ["completions", "embeddings", "models"]
	Status     Status            `json:"status"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
	LastUsedAt *time.Time        `json:"last_used_at,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// Status represents the key's current state.
type Status string

const (
	KeyActive  Status = "active"
	KeyRevoked Status = "revoked"
	KeyExpired Status = "expired"
)

// CreateInput is the input for creating an API key.
type CreateInput struct {
	TenantID string            `json:"tenant_id"`
	Name     string            `json:"name"`
	Scopes   []string          `json:"scopes,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Service manages API key lifecycle.
type Service interface {
	// Create generates a new API key. Returns the full key (only time it's visible).
	Create(ctx context.Context, input *CreateInput) (*APIKey, string, error)

	// Validate checks a raw API key and returns the associated key record.
	Validate(ctx context.Context, rawKey string) (*APIKey, error)

	// Revoke deactivates an API key.
	Revoke(ctx context.Context, id string) error

	// List returns keys for a tenant (hashed, never shows full key).
	List(ctx context.Context, tenantID string) ([]*APIKey, error)

	// Rotate creates a new key and revokes the old one atomically.
	Rotate(ctx context.Context, oldKeyID string) (*APIKey, string, error)
}

// ListOptions configures key listing. An empty TenantID lists every
// tenant's keys. Lists are newest first and cursor paged.
type ListOptions struct {
	TenantID string `json:"tenant_id,omitempty"`
	Status   Status `json:"status,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Cursor   string `json:"cursor,omitempty"`
}

// ListResult is one page of keys. NextCursor is "" on the last page.
type ListResult struct {
	Items      []*APIKey `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

// Store is the persistence interface for API keys.
// Finders and Update return ErrNotFound when no key matches; Delete of an unknown id is a no-op.
type Store interface {
	Insert(ctx context.Context, k *APIKey) error
	FindByID(ctx context.Context, id string) (*APIKey, error)
	FindByPrefix(ctx context.Context, prefix string) (*APIKey, error)
	Update(ctx context.Context, k *APIKey) error
	Delete(ctx context.Context, id string) error
	ListByTenant(ctx context.Context, tenantID string) ([]*APIKey, error)
	List(ctx context.Context, opts *ListOptions) (*ListResult, error)
}
