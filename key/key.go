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
	Hash       string            `json:"-"`      // SHA-256 of the raw key, hex (never exposed). The raw key carries 256 random bits, so an unsalted hash is enough.
	Scopes     []string          `json:"scopes"` // ["completions", "embeddings", "models"]
	Status     Status            `json:"status"`
	ExpiresAt  *time.Time        `json:"expires_at,omitempty"`
	LastUsedAt *time.Time        `json:"last_used_at,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at"`
}

// The scopes a key can hold. A key with no scopes given gets completions,
// embeddings and models. Admin is cross-tenant: an admin key administers
// every tenant, whichever tenant it belongs to.
const (
	ScopeCompletions = "completions"
	ScopeEmbeddings  = "embeddings"
	ScopeModels      = "models"
	ScopeAdmin       = "admin"
)

// KnownScope reports whether s is one of the scopes above.
func KnownScope(s string) bool {
	switch s {
	case ScopeCompletions, ScopeEmbeddings, ScopeModels, ScopeAdmin:
		return true
	}
	return false
}

// Status represents the key's current state.
type Status string

const (
	KeyActive  Status = "active"
	KeyRevoked Status = "revoked"
	KeyExpired Status = "expired"
)

// Effective returns the status k has at now. A key stored active whose
// ExpiresAt is at or before now is expired; every other status is as stored.
// It is the one rule for expiry: the service and every store use it, and the
// SQL and Mongo filters mirror it.
func Effective(k *APIKey, now time.Time) Status {
	if k.Status == KeyActive && k.ExpiresAt != nil && !k.ExpiresAt.After(now) {
		return KeyExpired
	}
	return k.Status
}

// CreateInput is the input for creating an API key.
type CreateInput struct {
	TenantID string            `json:"tenant_id"`
	Name     string            `json:"name"`
	Scopes   []string          `json:"scopes,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
	// ExpiresAt, when set, must be in the future.
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

// Service manages API key lifecycle.
type Service interface {
	// Create generates a new API key. Returns the full key (only time it's visible).
	Create(ctx context.Context, input *CreateInput) (*APIKey, string, error)

	// Validate checks a raw API key and returns the associated key record.
	Validate(ctx context.Context, rawKey string) (*APIKey, error)

	// Get returns one key, with its expiry derived: an active key whose
	// ExpiresAt has passed reads expired. Every store derives expiry the same
	// way in its List and Count, so the status filter and the items agree.
	Get(ctx context.Context, id string) (*APIKey, error)

	// Revoke deactivates an API key.
	Revoke(ctx context.Context, id string) error

	// List returns keys for a tenant (hashed, never shows full key).
	List(ctx context.Context, tenantID string) ([]*APIKey, error)

	// ListPage returns one page of keys, newest first. The store applies the
	// status filter with expiry derived, and every item carries its effective
	// status: an active key whose ExpiresAt has passed is expired.
	ListPage(ctx context.Context, opts *ListOptions) (*ListResult, error)

	// Count returns how many keys match opts, with the same filter semantics
	// as ListPage. Limit and Cursor are ignored.
	Count(ctx context.Context, opts *ListOptions) (int, error)

	// Rotate creates a replacement for an active key and revokes the old one.
	// It is not atomic across backends: if the revoke fails, the new key is
	// revoked too (best effort) and the error is returned.
	Rotate(ctx context.Context, oldKeyID string) (*APIKey, string, error)
}

// ListOptions configures key listing. An empty TenantID lists every
// tenant's keys. Lists are newest first and cursor paged.
type ListOptions struct {
	TenantID string `json:"tenant_id,omitempty"`
	Status   Status `json:"status,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Cursor   string `json:"cursor,omitempty"`

	// Now is the clock reading the status filter derives expiry against.
	// The zero value means the wall clock. key.Service sets it from its own
	// clock, so ListPage and Count agree with Get under WithClock.
	Now time.Time `json:"-"`
}

// At returns the time expiry is derived against: Now, or the wall clock when
// Now is unset.
func (o *ListOptions) At() time.Time {
	if o.Now.IsZero() {
		return time.Now().UTC()
	}
	return o.Now.UTC()
}

// ListResult is one page of keys. NextCursor is "" on the last page.
type ListResult struct {
	Items      []*APIKey `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

// Store is the persistence interface for API keys.
// Finders and Update return ErrNotFound when no key matches; Delete of an unknown id is a no-op.
//
// List and Count derive expiry. Against a clock reading now:
//   - active means stored active and (no ExpiresAt or ExpiresAt after now);
//   - expired means stored expired, or stored active with ExpiresAt at or before now;
//   - revoked means stored revoked.
//
// Every key List returns carries its effective status (see Effective).
type Store interface {
	Insert(ctx context.Context, k *APIKey) error
	FindByID(ctx context.Context, id string) (*APIKey, error)
	// FindByPrefix returns every key whose prefix is prefix, in any status.
	// Prefixes are not unique, so a caller must check the hash. None found
	// is an empty slice and a nil error.
	FindByPrefix(ctx context.Context, prefix string) ([]*APIKey, error)
	// TouchLastUsed sets only LastUsedAt, so it can never undo a concurrent
	// revoke. It returns ErrNotFound when the key does not exist.
	TouchLastUsed(ctx context.Context, id string, at time.Time) error
	Update(ctx context.Context, k *APIKey) error
	Delete(ctx context.Context, id string) error
	ListByTenant(ctx context.Context, tenantID string) ([]*APIKey, error)
	List(ctx context.Context, opts *ListOptions) (*ListResult, error)
	// Count returns how many keys match opts, with List's filter semantics.
	// Limit and Cursor are ignored. nil opts counts every key.
	Count(ctx context.Context, opts *ListOptions) (int, error)
}
