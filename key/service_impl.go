package key

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
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
	if k != nil {
		k.Status = Effective(k, s.now())
	}
	return k
}

func (s *service) Create(ctx context.Context, input *CreateInput) (*APIKey, string, error) {
	rawBytes := make([]byte, 32)
	if _, err := rand.Read(rawBytes); err != nil {
		return nil, "", fmt.Errorf("nexus: generate key: %w", err)
	}
	rawKey := keyPrefix + hex.EncodeToString(rawBytes)
	k, err := s.build(ctx, input, rawKey)
	if err != nil {
		return nil, "", err
	}
	if err := s.insert(ctx, k); err != nil {
		return nil, "", err
	}
	return k, rawKey, nil
}

// Ensure creates the key whose raw value is rawKey, unless a key with that
// value exists already, in any status. A revoked key stays revoked: Ensure
// never brings one back. Two callers racing to create the same key get the
// same row: the loser's insert is refused as a duplicate and it returns the
// winner's. The error text never carries rawKey.
func (s *service) Ensure(ctx context.Context, rawKey string, input *CreateInput) (*APIKey, bool, error) {
	if !WellFormed(rawKey) {
		return nil, false, fmt.Errorf("%w: a key is %q followed by 64 lowercase hex digits", ErrInvalid, keyPrefix)
	}
	existing, err := s.find(ctx, rawKey)
	if err != nil && !errors.Is(err, ErrNotFound) {
		return nil, false, fmt.Errorf("nexus: look up key: %w", err)
	}
	if existing != nil {
		return s.derive(existing), false, nil
	}
	k, err := s.build(ctx, input, rawKey)
	if err != nil {
		return nil, false, err
	}
	if err := s.insert(ctx, k); err != nil {
		if !errors.Is(err, ErrDuplicate) {
			return nil, false, err
		}
		// Another caller inserted this key between our lookup and our
		// insert. Its row is the one: report it, create nothing.
		won, findErr := s.find(ctx, rawKey)
		if findErr != nil {
			return nil, false, err
		}
		return s.derive(won), false, nil
	}
	return k, true, nil
}

// build checks input and returns the record for a new key whose raw value is
// rawKey. It stores nothing.
func (s *service) build(ctx context.Context, input *CreateInput, rawKey string) (*APIKey, error) {
	if input == nil || input.Name == "" {
		return nil, fmt.Errorf("%w: name is required", ErrInvalid)
	}
	tid, err := id.ParseTenantID(input.TenantID)
	if err != nil {
		return nil, fmt.Errorf("%w: tenant id: %w", ErrInvalid, err)
	}
	if input.ExpiresAt != nil && !input.ExpiresAt.After(s.now()) {
		return nil, fmt.Errorf("%w: expires_at is in the past", ErrInvalid)
	}
	for _, sc := range input.Scopes {
		if !KnownScope(sc) {
			return nil, fmt.Errorf("%w: unknown scope %q (want completions, embeddings, models or admin)", ErrInvalid, sc)
		}
	}
	if s.tenants != nil {
		if _, err := s.tenants.FindByID(ctx, tid.String()); err != nil {
			return nil, fmt.Errorf("nexus: key for tenant %s: %w", tid, err)
		}
	}
	scopes := input.Scopes
	if len(scopes) == 0 {
		scopes = []string{ScopeCompletions, ScopeEmbeddings, ScopeModels}
	}
	k := &APIKey{
		ID:        id.NewKeyID(),
		TenantID:  tid,
		Name:      input.Name,
		Prefix:    rawKey[:prefixLen],
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
	return k, nil
}

// insert stores a built key and reports it.
func (s *service) insert(ctx context.Context, k *APIKey) error {
	if err := s.store.Insert(ctx, k); err != nil {
		return err
	}
	if s.events != nil {
		s.events.EmitKeyCreated(ctx, k.ID, k.TenantID)
	}
	return nil
}

// find returns the key whose hash matches rawKey, in any status, or
// ErrNotFound. It touches nothing. rawKey must be well formed.
//
// The hash is unique in every store, but rows written before that, or by
// two replicas racing a first insert, can share one. Then find fails closed:
// a key that is not usable (revoked, or expired by its ExpiresAt) wins over
// an active one, whatever order the store returns them in, so revoking a key
// always takes effect.
func (s *service) find(ctx context.Context, rawKey string) (*APIKey, error) {
	candidates, err := s.store.FindByPrefix(ctx, rawKey[:prefixLen])
	if err != nil {
		return nil, err
	}
	want := []byte(hashKey(rawKey))
	var match *APIKey
	now := s.now()
	// Compare against every candidate, in constant time, and keep going
	// after a match, so timing says nothing about which key matched.
	for _, k := range candidates {
		if subtle.ConstantTimeCompare([]byte(k.Hash), want) != 1 {
			continue
		}
		if match == nil || (Effective(match, now) == KeyActive && Effective(k, now) != KeyActive) {
			match = k
		}
	}
	if match == nil {
		return nil, ErrNotFound
	}
	return match, nil
}

func (s *service) Validate(ctx context.Context, rawKey string) (*APIKey, error) {
	// A value Create could not have made never reaches the store: a store
	// may reject odd bytes (Postgres refuses invalid UTF-8) and the caller
	// would see an outage instead of a wrong key.
	if !WellFormed(rawKey) {
		return nil, ErrNotFound
	}
	match, err := s.find(ctx, rawKey)
	if err != nil {
		return nil, err
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

// ListPage delegates to the store, which filters on status and derives expiry
// against the service's clock.
func (s *service) ListPage(ctx context.Context, opts *ListOptions) (*ListResult, error) {
	return s.store.List(ctx, s.withClock(opts))
}

// Count delegates to the store, with the same filter semantics as ListPage.
func (s *service) Count(ctx context.Context, opts *ListOptions) (int, error) {
	return s.store.Count(ctx, s.withClock(opts))
}

// withClock returns a copy of opts that derives expiry against the service's
// clock, unless the caller already set one.
func (s *service) withClock(opts *ListOptions) *ListOptions {
	var o ListOptions
	if opts != nil {
		o = *opts
	}
	if o.Now.IsZero() {
		o.Now = s.now()
	}
	return &o
}

const rotatedSuffix = " (rotated)"

// Rotate creates a replacement for an active key and revokes the old one.
// It is not atomic across backends: if the revoke fails, the new key is
// revoked too, and the error is returned. If that rollback also fails, the
// returned error names the replacement key that is still active.
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
		first := fmt.Errorf("nexus: revoke the rotated key: %w", err)
		if rbErr := s.Revoke(ctx, n.ID.String()); rbErr != nil {
			// Nobody holds the replacement's raw value, yet it is live. Say so.
			return nil, "", errors.Join(first,
				fmt.Errorf("nexus: the replacement key %s is still active and must be revoked: %w", n.ID, rbErr))
		}
		return nil, "", first
	}
	return n, raw, nil
}

// The shape of a raw key: "nxs_" and 64 lowercase hex digits. The stored
// prefix is the first prefixLen characters.
const (
	keyPrefix = "nxs_"
	prefixLen = 12
)

// WellFormed reports whether raw has the shape Create gives a key: "nxs_"
// and 64 lowercase hex digits.
func WellFormed(raw string) bool {
	if len(raw) != len(keyPrefix)+64 || raw[:len(keyPrefix)] != keyPrefix {
		return false
	}
	for i := len(keyPrefix); i < len(raw); i++ {
		c := raw[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// hashKey creates a SHA-256 hash of the API key.
func hashKey(rawKey string) string {
	h := sha256.Sum256([]byte(rawKey))
	return hex.EncodeToString(h[:])
}
