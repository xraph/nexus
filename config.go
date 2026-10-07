package nexus

import "time"

// Config holds the gateway configuration.
type Config struct {
	// BasePath is the HTTP base path for routes (default: "/nexus").
	BasePath string

	// DefaultTimeout is the default timeout for provider requests (default: 30s).
	DefaultTimeout time.Duration

	// DefaultMaxRetries is the default number of retries per request (default: 2).
	DefaultMaxRetries int

	// EnableUsage enables usage tracking (default: true).
	EnableUsage bool

	// EnableCache enables caching (default: false, must provide cache).
	EnableCache bool

	// RequireAPIKey makes the HTTP edges (api, proxy) refuse a request
	// without a valid nxs_ key. Default true. False keeps an open gateway
	// for local work; a key that is presented is still checked.
	RequireAPIKey bool

	// GlobalRateLimit is the global rate limit in requests per minute (0 = unlimited).
	GlobalRateLimit int

	// LogLevel is the log level for the internal logger (default: "info").
	LogLevel string
}

// DefaultConfig returns the default gateway configuration.
func DefaultConfig() *Config {
	return &Config{
		BasePath:          "/nexus",
		DefaultTimeout:    30 * time.Second,
		DefaultMaxRetries: 2,
		EnableUsage:       true,
		EnableCache:       false,
		RequireAPIKey:     true,
		GlobalRateLimit:   0,
		LogLevel:          "info",
	}
}
