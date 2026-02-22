package key

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/xraph/nexus/id"
)

type service struct {
	store Store
}

// NewService creates a new API key service.
func NewService(store Store) Service {
	return &service{store: store}
}

func (s *service) Create(ctx context.Context, input *CreateInput) (*APIKey, string, error) {
	if input.TenantID == "" {
		return nil, "", errors.New("nexus: tenant_id is required")
	}
	if input.Name == "" {
		return nil, "", errors.New("nexus: key name is required")
	}

	// Generate a random API key: nxs_<32 random hex chars>
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, "", fmt.Errorf("nexus: failed to generate key: %w", err)
	}
	rawKey := "nxs_" + hex.EncodeToString(rawBytes)

	// Hash for storage
	hash := hashKey(rawKey)

	// Prefix for lookup (first 8 chars of the random part)
	prefix := rawKey[:12] // "nxs_" + 8 hex chars

	scopes := input.Scopes
	if len(scopes) == 0 {
		scopes = []string{"completions", "embeddings", "models"}
	}

	k := &APIKey{
		ID:        id.NewKeyID(),
		TenantID:  id.MustParseTenantID(input.TenantID),
		Name:      input.Name,
		Prefix:    prefix,
		Hash:      hash,
		Scopes:    scopes,
		Status:    KeyActive,
		Metadata:  input.Metadata,
		CreatedAt: time.Now(),
	}

	if k.Metadata == nil {
		k.Metadata = make(map[string]string)
	}

	if err := s.store.Insert(ctx, k); err != nil {
		return nil, "", err
	}

	return k, rawKey, nil
}

func (s *service) Validate(ctx context.Context, rawKey string) (*APIKey, error) {
	if len(rawKey) < 12 {
		return nil, errors.New("nexus: invalid API key format")
	}

	prefix := rawKey[:12]
	k, err := s.store.FindByPrefix(ctx, prefix)
	if err != nil {
		return nil, err
	}

	// Verify hash
	if hashKey(rawKey) != k.Hash {
		return nil, errors.New("nexus: invalid API key")
	}

	// Check status
	if k.Status == KeyRevoked {
		return nil, errors.New("nexus: API key revoked")
	}
	if k.ExpiresAt != nil && k.ExpiresAt.Before(time.Now()) {
		return nil, errors.New("nexus: API key expired")
	}

	// Update last used
	now := time.Now()
	k.LastUsedAt = &now
	_ = s.store.Update(ctx, k)

	return k, nil
}

func (s *service) Revoke(ctx context.Context, keyID string) error {
	k, err := s.store.FindByID(ctx, keyID)
	if err != nil {
		return err
	}
	k.Status = KeyRevoked
	return s.store.Update(ctx, k)
}

func (s *service) List(ctx context.Context, tenantID string) ([]*APIKey, error) {
	return s.store.ListByTenant(ctx, tenantID)
}

func (s *service) Rotate(ctx context.Context, oldKeyID string) (*APIKey, string, error) {
	old, err := s.store.FindByID(ctx, oldKeyID)
	if err != nil {
		return nil, "", fmt.Errorf("nexus: old key not found: %w", err)
	}

	// Create new key with same properties
	newKey, rawKey, err := s.Create(ctx, &CreateInput{
		TenantID: old.TenantID.String(),
		Name:     old.Name + " (rotated)",
		Scopes:   old.Scopes,
		Metadata: old.Metadata,
	})
	if err != nil {
		return nil, "", err
	}

	// Revoke old key
	old.Status = KeyRevoked
	_ = s.store.Update(ctx, old)

	return newKey, rawKey, nil
}

// hashKey creates a SHA-256 hash of the API key.
func hashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}
