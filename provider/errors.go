package provider

import "errors"

// ErrNotSupported is returned when a provider does not support an operation.
var ErrNotSupported = errors.New("nexus: operation not supported by provider")

// ErrMissingAPIKey is returned when a provider is invoked without a credential.
// Providers should return this (wrapped with their name) before making any
// network call, so the failure is a clear local error rather than a cryptic
// upstream 401 such as Anthropic's "x-api-key header is required".
var ErrMissingAPIKey = errors.New("nexus: provider API key is required")
