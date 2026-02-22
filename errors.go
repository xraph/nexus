package nexus

import "errors"

var (
	// Provider errors
	ErrProviderNotFound    = errors.New("nexus: provider not found")
	ErrProviderUnavailable = errors.New("nexus: provider unavailable")
	ErrModelNotSupported   = errors.New("nexus: model not supported by provider")
	ErrAllProvidersFailed  = errors.New("nexus: all providers failed")

	// Auth errors
	ErrUnauthorized  = errors.New("nexus: unauthorized")
	ErrAPIKeyInvalid = errors.New("nexus: invalid API key")
	ErrAPIKeyRevoked = errors.New("nexus: API key revoked")

	// Tenant errors
	ErrTenantNotFound = errors.New("nexus: tenant not found")
	ErrTenantDisabled = errors.New("nexus: tenant disabled")
	ErrQuotaExceeded  = errors.New("nexus: quota exceeded")
	ErrBudgetExceeded = errors.New("nexus: budget exceeded")

	// Rate limiting
	ErrRateLimited = errors.New("nexus: rate limited")

	// Guardrail errors
	ErrContentBlocked    = errors.New("nexus: content blocked by guardrail")
	ErrPIIDetected       = errors.New("nexus: PII detected in request")
	ErrInjectionDetected = errors.New("nexus: prompt injection detected")

	// Cache errors
	ErrCacheNotConfigured = errors.New("nexus: cache not configured")

	// Pipeline errors
	ErrPipelineAborted = errors.New("nexus: pipeline aborted")
	ErrCircuitOpen     = errors.New("nexus: circuit breaker open")

	// Context & tokens
	ErrContextOverflow     = errors.New("nexus: request exceeds context window")
	ErrTokenEstimateFailed = errors.New("nexus: token estimation failed")

	// Model aliases
	ErrAliasNotFound      = errors.New("nexus: model alias not found")
	ErrNoTargetsAvailable = errors.New("nexus: no alias targets available")

	// Extended thinking
	ErrThinkingNotSupported = errors.New("nexus: provider does not support extended thinking")

	// Batch
	ErrBatchTooLarge = errors.New("nexus: batch exceeds maximum size")
	ErrBatchNotFound = errors.New("nexus: batch job not found")

	// Credentials
	ErrCredentialExpired  = errors.New("nexus: provider credential expired")
	ErrCredentialNotFound = errors.New("nexus: provider credential not found")
)
