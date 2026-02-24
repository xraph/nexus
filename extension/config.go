package extension

import "time"

// Config holds the Nexus Forge extension configuration.
// Fields can be set programmatically via Option functions or loaded from
// YAML configuration files (under "extensions.nexus" or "nexus" keys).
type Config struct {
	// DisableRoutes prevents HTTP route registration.
	DisableRoutes bool `json:"disable_routes" mapstructure:"disable_routes" yaml:"disable_routes"`

	// DisableMigrate prevents auto-migration of the persistence store on start.
	DisableMigrate bool `json:"disable_migrate" mapstructure:"disable_migrate" yaml:"disable_migrate"`

	// BasePath is the URL prefix for gateway routes (default: "/ai").
	BasePath string `json:"base_path" mapstructure:"base_path" yaml:"base_path"`

	// DefaultTimeout is the default timeout for provider requests.
	DefaultTimeout time.Duration `json:"default_timeout" mapstructure:"default_timeout" yaml:"default_timeout"`

	// DefaultMaxRetries is the default number of retries per request.
	DefaultMaxRetries int `json:"default_max_retries" mapstructure:"default_max_retries" yaml:"default_max_retries"`

	// GlobalRateLimit is the global rate limit in requests per minute (0 = unlimited).
	GlobalRateLimit int `json:"global_rate_limit" mapstructure:"global_rate_limit" yaml:"global_rate_limit"`

	// LogLevel is the log level for the gateway's internal logger (default: "info").
	LogLevel string `json:"log_level" mapstructure:"log_level" yaml:"log_level"`

	// EnableUsage enables usage tracking (default: true).
	EnableUsage *bool `json:"enable_usage" mapstructure:"enable_usage" yaml:"enable_usage"`

	// EnableCache enables response caching (default: false).
	EnableCache bool `json:"enable_cache" mapstructure:"enable_cache" yaml:"enable_cache"`

	// RequireConfig requires config to be present in YAML files.
	// If true and no config is found, Register returns an error.
	RequireConfig bool `json:"-" yaml:"-"`
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	enableUsage := true
	return Config{
		BasePath:          "/ai",
		DefaultTimeout:    30 * time.Second,
		DefaultMaxRetries: 2,
		GlobalRateLimit:   0,
		LogLevel:          "info",
		EnableUsage:       &enableUsage,
		EnableCache:       false,
	}
}
