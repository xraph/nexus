package key

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/xraph/nexus/id"
)

type service struct {
	store   Store
	tenants TenantFinder
	events  Events
	now     func() time.Time
}

func NewService(store Store, opts ...Option) Service {
	s := &service{store: store, now: time.Now}
	for _, o := range opts {
		o(s)
	}
	return s
}

// derive reports an active key whose expiry has passed as expired.
func (s *service) derive(k *APIKey) *APIKey {
	if k != nil && k.Status == KeyActive && k.ExpiresAt != nil && !k.ExpiresAt.After(s.now()) {
		k.Status = KeyExpired
	}
	return k
}

func (s *service) Create(ctx context.Context, input *CreateInput) (*APIKey, string, error) {
	if input == nil || input.Name == "" {
		return nil, "", fmt.Errorf("%w: name is required", ErrInvalid)
	}
	tid, err := id.ParseTenantID(input.TenantID)
	if err != nil {
		return nil, "", fmt.Errorf("%w: tenant id: %w", ErrInvalid, err)
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return nil, "", fmt.Errorf("%w: expires_at is in the past", ErrInvalid)
	}
	if s.tenants != nil {
		if _, err := s.tenants.FindByID(ctx, tid.String()); err != nil {
			return nil, "", fmt.Errorf("nexus: key for tenant %s: %w", tid, err)
		}
	}
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, "", fmt.Errorf("nexus: generate key: %w", err)
	}
	rawKey := "nxs_" + hex.EncodeToString(rawBytes)
	scopes := input.Scopes
	if len(scopes) == 0 {
		scopes = []string{"completions", "embeddings", "models"}
	}
	k := &APIKey{
		ID:        id.NewKeyID(),
		TenantID:  tid,
		Name:      input.Name,
		Prefix:    rawKey[:12],
		Hash:      hashKey(rawKey),
		Scopes:    scopes,
		Status:    KeyActive,
		ExpiresAt: input.ExpiresAt,
		Metadata:  input.Metadata,
		CreatedAt: s.now(),
	}
	if k.Metadata == nil {
		k.Metadata = map[string]string{}
	}
	if err := s.store.Insert(ctx, k); err != nil {
		return nil, "", err
	}
	if s.events != nil {
		s.events.EmitKeyCreated(ctx, k.ID, k.TenantID)
	}
	return k, rawKey, nil
}

func (s *service) Validate(ctx context.Context, rawKey string) (*APIKey, error) {
	if len(rawKey) < 12 {
		return nil, ErrNotFound
	}
	candidates, err := s.store.FindByPrefix(ctx, rawKey[:12])
	if err != nil {
		return nil, err
	}
	want := []byte(hashKey(rawKey))
	var match *APIKey
	// Compare against every candidate, in constant time, and keep going
	// after a match, so timing says nothing about which key matched.
	for _, k := range candidates {
		if subtle.ConstantTimeCompare([]byte(k.Hash), want) == 1 {
			match = k
		}
	}
	if match == nil {
		return nil, ErrNotFound
	}
	s.derive(match)
	switch match.Status {
	case KeyRevoked:
		return nil, ErrRevoked
	case KeyExpired:
		return nil, ErrExpired
	}
	now := s.now()
	if match.LastUsedAt == nil || now.Sub(*match.LastUsedAt) >= time.Minute {
		// Best effort: a failed touch must not refuse a valid key.
		if err := s.store.TouchLastUsed(ctx, match.ID.String(), now); err == nil {
			match.LastUsedAt = &now
		}
	}
	return match, nil
}

func (s *service) Get(ctx context.Context, keyID string) (*APIKey, error) {
	k, err := s.store.FindByID(ctx, keyID)
	if err != nil {
		return nil, err
	}
	return s.derive(k), nil
}

func (s *service) Revoke(ctx context.Context, keyID string) error {
	k, err := s.store.FindByID(ctx, keyID)
	if err != nil {
		return err
	}
	if k.Status == KeyRevoked {
		return nil
	}
	k.Status = KeyRevoked
	if err := s.store.Update(ctx, k); err != nil {
		return err
	}
	if s.events != nil {
		s.events.EmitKeyRevoked(ctx, k.ID)
	}
	return nil
}

func (s *service) List(ctx context.Context, tenantID string) ([]*APIKey, error) {
	keys, err := s.store.ListByTenant(ctx, tenantID)
	for _, k := range keys {
		s.derive(k)
	}
	return keys, err
}

const rotatedSuffix = " (rotated)"

// Rotate creates a replacement for an active key and revokes the old one.
// It is not atomic across backends: if the revoke fails, the new key is
// revoked too (best effort) and the error is returned, so no key the caller
// never saw is left active.
func (s *service) Rotate(ctx context.Context, oldKeyID string) (*APIKey, string, error) {
	old, err := s.Get(ctx, oldKeyID)
	if err != nil {
		return nil, "", fmt.Errorf("nexus: old key: %w", err)
	}
	if old.Status != KeyActive {
		return nil, "", fmt.Errorf("%w: only an active key can be rotated (this one is %s)", ErrInvalid, old.Status)
	}
	n, raw, err := s.Create(ctx, &CreateInput{
		TenantID:  old.TenantID.String(),
		Name:      strings.TrimSuffix(old.Name, rotatedSuffix) + rotatedSuffix,
		Scopes:    old.Scopes,
		ExpiresAt: old.ExpiresAt,
		Metadata:  old.Metadata,
	})
	if err != nil {
		return nil, "", err
	}
	if err := s.Revoke(ctx, old.ID.String()); err != nil {
		_ = s.Revoke(ctx, n.ID.String()) //nolint:errcheck // best-effort undo; the revoke error is the one returned
		return nil, "", fmt.Errorf("nexus: revoke the rotated key: %w", err)
	}
	return n, raw, nil
}

// hashKey creates a SHA-256 hash of the API key.
func hashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}
