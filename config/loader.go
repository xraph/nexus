package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Load reads configuration from a JSON file.
// YAML support requires an external package and is deferred.
func Load(path string) (*GatewayConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("nexus: failed to read config: %w", err)
	}

	var cfg GatewayConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("nexus: failed to parse config: %w", err)
	}

	// Apply environment variable overrides
	applyEnvOverrides(&cfg)

	return &cfg, nil
}

// applyEnvOverrides reads NEXUS_* environment variables and overrides config.
func applyEnvOverrides(cfg *GatewayConfig) {
	if v := os.Getenv("NEXUS_BASE_PATH"); v != "" {
		cfg.Server.BasePath = v
	}
	if v := os.Getenv("NEXUS_LOG_LEVEL"); v != "" {
		cfg.Server.LogLevel = v
	}

	// Provider API keys from env: NEXUS_PROVIDER_<NAME>_API_KEY
	for i := range cfg.Providers {
		envKey := fmt.Sprintf("NEXUS_PROVIDER_%s_API_KEY", strings.ToUpper(cfg.Providers[i].Name))
		if v := os.Getenv(envKey); v != "" {
			cfg.Providers[i].APIKey = v
		}
	}
}
