// Package config provides configuration loading for Nexus.
// It supports YAML config files and environment variable overrides.
package config

import "time"

// GatewayConfig is the full configuration schema for nexus.yaml.
type GatewayConfig struct {
	// Server
	Server ServerConfig `json:"server" yaml:"server"`

	// Provider configurations
	Providers []ProviderConfig `json:"providers" yaml:"providers"`

	// Routing
	Routing RoutingConfig `json:"routing" yaml:"routing"`

	// Cache
	Cache CacheConfig `json:"cache" yaml:"cache"`

	// Guardrails
	Guardrails GuardrailConfig `json:"guardrails" yaml:"guardrails"`

	// Model aliases
	Aliases []AliasConfig `json:"aliases" yaml:"aliases"`

	// Resilience
	Resilience ResilienceConfig `json:"resilience" yaml:"resilience"`
}

// ServerConfig configures the HTTP server.
type ServerConfig struct {
	BasePath string `json:"base_path" yaml:"base_path"`
	Port     int    `json:"port" yaml:"port"`
	LogLevel string `json:"log_level" yaml:"log_level"`
}

// ProviderConfig configures a single provider.
type ProviderConfig struct {
	Name    string   `json:"name" yaml:"name"`
	Type    string   `json:"type" yaml:"type"` // "openai", "anthropic", "opencompat", "groq", "gemini", etc.
	APIKey  string   `json:"api_key" yaml:"api_key"`
	BaseURL string   `json:"base_url,omitempty" yaml:"base_url"`
	Models  []string `json:"models,omitempty" yaml:"models"`

	// Azure OpenAI specific
	ResourceName string `json:"resource_name,omitempty" yaml:"resource_name"`
	DeploymentID string `json:"deployment_id,omitempty" yaml:"deployment_id"`
	APIVersion   string `json:"api_version,omitempty" yaml:"api_version"`

	// AWS Bedrock specific
	Region         string `json:"region,omitempty" yaml:"region"`
	AccessKeyID    string `json:"access_key_id,omitempty" yaml:"access_key_id"`
	SecretAccessKey string `json:"secret_access_key,omitempty" yaml:"secret_access_key"`
	SessionToken   string `json:"session_token,omitempty" yaml:"session_token"`

	// Google Vertex AI specific
	ProjectID      string `json:"project_id,omitempty" yaml:"project_id"`
	Location       string `json:"location,omitempty" yaml:"location"`
	AccessToken    string `json:"access_token,omitempty" yaml:"access_token"`
	CredentialFile string `json:"credential_file,omitempty" yaml:"credential_file"`
}

// RoutingConfig configures the routing strategy.
type RoutingConfig struct {
	Strategy string             `json:"strategy" yaml:"strategy"` // "priority", "cost", "latency", "round_robin", "weighted"
	Priority []string           `json:"priority,omitempty" yaml:"priority"`
	Weights  map[string]float64 `json:"weights,omitempty" yaml:"weights"`
}

// CacheConfig configures caching.
type CacheConfig struct {
	Enabled bool          `json:"enabled" yaml:"enabled"`
	Type    string        `json:"type" yaml:"type"` // "memory", "redis"
	TTL     time.Duration `json:"ttl" yaml:"ttl"`
	MaxSize int           `json:"max_size" yaml:"max_size"`
}

// GuardrailConfig configures guardrails.
type GuardrailConfig struct {
	PII       PIIConfig `json:"pii,omitempty" yaml:"pii"`
	Injection bool      `json:"injection" yaml:"injection"`
	Blocklist []string  `json:"blocklist,omitempty" yaml:"blocklist"`
}

// PIIConfig configures PII detection.
type PIIConfig struct {
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Action  string `json:"action" yaml:"action"` // "block", "redact", "warn"
}

// AliasConfig configures a model alias.
type AliasConfig struct {
	Name    string              `json:"name" yaml:"name"`
	Targets []AliasTargetConfig `json:"targets" yaml:"targets"`
}

// AliasTargetConfig maps to a provider+model.
type AliasTargetConfig struct {
	Provider string  `json:"provider" yaml:"provider"`
	Model    string  `json:"model" yaml:"model"`
	Weight   float64 `json:"weight,omitempty" yaml:"weight"`
}

// ResilienceConfig configures retry and fallback behavior.
type ResilienceConfig struct {
	MaxRetries int           `json:"max_retries" yaml:"max_retries"`
	RetryDelay time.Duration `json:"retry_delay" yaml:"retry_delay"`
	Timeout    time.Duration `json:"timeout" yaml:"timeout"`
}
