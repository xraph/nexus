package nexus

import "time"

// Config holds the gateway configuration.
type Config struct {
	// BasePath is the HTTP base path for routes (default: "/ai").
	BasePath string

	// DefaultTimeout is the default timeout for provider requests (default: 30s).
	DefaultTimeout time.Duration

	// DefaultMaxRetries is the default number of retries per request (default: 2).
	DefaultMaxRetries int

	// EnableUsage enables usage tracking (default: true).
	EnableUsage bool

	// EnableCache enables caching (default: false, must provide cache).
	EnableCache bool

	// GlobalRateLimit is the global rate limit in requests per minute (0 = unlimited).
	GlobalRateLimit int

	// LogLevel is the log level for the internal logger (default: "info").
	LogLevel string
}

// DefaultConfig returns the default gateway configuration.
func DefaultConfig() *Config {
	return &Config{
		BasePath:          "/ai",
		DefaultTimeout:    30 * time.Second,
		DefaultMaxRetries: 2,
		EnableUsage:       true,
		EnableCache:       false,
		GlobalRateLimit:   0,
		LogLevel:          "info",
	}
}
