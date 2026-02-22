package provider

import (
	"context"
	"errors"
	"os"
	"strings"
)

// CredentialProvider retrieves API keys or tokens for providers at runtime.
// This allows integration with secret managers (Vault, AWS Secrets, etc.)
// instead of hard-coding API keys.
type CredentialProvider interface {
	// GetCredential returns the current credential (API key) for a provider.
	GetCredential(ctx context.Context, providerName string) (string, error)
}

// StaticCredential always returns the same key. Useful for testing.
type StaticCredential struct {
	credentials map[string]string
}

// NewStaticCredential creates a credential provider from a static map.
func NewStaticCredential(credentials map[string]string) *StaticCredential {
	return &StaticCredential{credentials: credentials}
}

func (sc *StaticCredential) GetCredential(_ context.Context, providerName string) (string, error) {
	key, ok := sc.credentials[providerName]
	if !ok {
		return "", errCredentialNotFound
	}
	return key, nil
}

var errCredentialNotFound = errors.New("nexus: provider credential not found")

// EnvCredential reads credentials from environment variables.
type EnvCredential struct {
	envPrefix string // e.g., "NEXUS_" → looks up NEXUS_OPENAI_API_KEY
}

// NewEnvCredential creates a credential provider that reads from env vars.
// The environment variable name is formed as: {prefix}{PROVIDER_NAME}_API_KEY.
func NewEnvCredential(prefix string) *EnvCredential {
	return &EnvCredential{envPrefix: prefix}
}

func (ec *EnvCredential) GetCredential(_ context.Context, providerName string) (string, error) {
	key := os.Getenv(ec.envPrefix + strings.ToUpper(providerName) + "_API_KEY")
	if key == "" {
		return "", errCredentialNotFound
	}
	return key, nil
}
