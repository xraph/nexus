// Package id provides TypeID-based identity types for all Nexus entities.
//
// Every entity in Nexus gets a type-prefixed, K-sortable, UUIDv7-based
// identifier. IDs are compile-time safe — you cannot pass a TenantID where a
// KeyID is expected.
//
// Examples:
//
//	tenant_01h2xcejqtf2nbrexx3vqjhp41
//	key_01h455vb4pex5vsknk084sn02q
//	usage_01h6rz1g6p2m3q9xvz1t2b7c4d
//	req_01h9a1b2c3d4e5f6g7h8j9k0m1
package id

import "go.jetify.com/typeid"

// ──────────────────────────────────────────────────
// Prefix types — each entity has its own prefix
// ──────────────────────────────────────────────────

// TenantPrefix is the TypeID prefix for tenants.
type TenantPrefix struct{}

// Prefix returns "tenant".
func (TenantPrefix) Prefix() string { return "tenant" }

// KeyPrefix is the TypeID prefix for API keys.
type KeyPrefix struct{}

// Prefix returns "key".
func (KeyPrefix) Prefix() string { return "key" }

// UsagePrefix is the TypeID prefix for usage records.
type UsagePrefix struct{}

// Prefix returns "usage".
func (UsagePrefix) Prefix() string { return "usage" }

// RequestPrefix is the TypeID prefix for requests.
type RequestPrefix struct{}

// Prefix returns "req".
func (RequestPrefix) Prefix() string { return "req" }

// ──────────────────────────────────────────────────
// Typed ID aliases — compile-time safe
// ──────────────────────────────────────────────────

// TenantID is a type-safe identifier for tenants (prefix: "tenant").
type TenantID = typeid.TypeID[TenantPrefix]

// KeyID is a type-safe identifier for API keys (prefix: "key").
type KeyID = typeid.TypeID[KeyPrefix]

// UsageID is a type-safe identifier for usage records (prefix: "usage").
type UsageID = typeid.TypeID[UsagePrefix]

// RequestID is a type-safe identifier for requests (prefix: "req").
type RequestID = typeid.TypeID[RequestPrefix]

// AnyID is a TypeID that accepts any valid prefix. Use for cases where
// the prefix is dynamic or unknown at compile time.
type AnyID = typeid.AnyID

// ──────────────────────────────────────────────────
// Constructors
// ──────────────────────────────────────────────────

// NewTenantID returns a new random TenantID.
func NewTenantID() TenantID { return must(typeid.New[TenantID]()) }

// NewKeyID returns a new random KeyID.
func NewKeyID() KeyID { return must(typeid.New[KeyID]()) }

// NewUsageID returns a new random UsageID.
func NewUsageID() UsageID { return must(typeid.New[UsageID]()) }

// NewRequestID returns a new random RequestID.
func NewRequestID() RequestID { return must(typeid.New[RequestID]()) }

// ──────────────────────────────────────────────────
// Parsing (type-safe: ParseTenantID("key_01h...") fails)
// ──────────────────────────────────────────────────

// ParseTenantID parses a string into a TenantID. Returns an error if the
// prefix is not "tenant" or the suffix is invalid.
func ParseTenantID(s string) (TenantID, error) { return typeid.Parse[TenantID](s) }

// ParseKeyID parses a string into a KeyID.
func ParseKeyID(s string) (KeyID, error) { return typeid.Parse[KeyID](s) }

// ParseUsageID parses a string into a UsageID.
func ParseUsageID(s string) (UsageID, error) { return typeid.Parse[UsageID](s) }

// ParseRequestID parses a string into a RequestID.
func ParseRequestID(s string) (RequestID, error) { return typeid.Parse[RequestID](s) }

// ParseAny parses a string into an AnyID, accepting any valid prefix.
func ParseAny(s string) (AnyID, error) { return typeid.FromString(s) }

// ──────────────────────────────────────────────────
// Must parsers (panic on error — use only for trusted input)
// ──────────────────────────────────────────────────

// MustParseTenantID parses a TenantID or panics.
func MustParseTenantID(s string) TenantID { return must(ParseTenantID(s)) }

// MustParseKeyID parses a KeyID or panics.
func MustParseKeyID(s string) KeyID { return must(ParseKeyID(s)) }

// MustParseUsageID parses a UsageID or panics.
func MustParseUsageID(s string) UsageID { return must(ParseUsageID(s)) }

// MustParseRequestID parses a RequestID or panics.
func MustParseRequestID(s string) RequestID { return must(ParseRequestID(s)) }

// ──────────────────────────────────────────────────
// Helpers
// ──────────────────────────────────────────────────

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
