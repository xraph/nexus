package contract

import (
	"context"

	nexus "github.com/xraph/nexus"
)

type settingsResponse struct {
	BasePath            string `json:"basePath"`
	DefaultTimeoutMS    int64  `json:"defaultTimeoutMs"`
	DefaultMaxRetries   int    `json:"defaultMaxRetries"`
	GlobalRateLimit     int    `json:"globalRateLimit"`
	UsageEnabled        bool   `json:"usageEnabled"`
	CacheEnabled        bool   `json:"cacheEnabled"`
	RequireAPIKey       bool   `json:"requireApiKey"`
	LogLevel            string `json:"logLevel"`
	AuthenticationScope string `json:"authenticationScope"`
}

func settingsGet(_ context.Context, gw *nexus.Gateway, _ struct{}) (settingsResponse, error) {
	c := gw.Config()
	return settingsResponse{
		BasePath: c.BasePath, DefaultTimeoutMS: c.DefaultTimeout.Milliseconds(),
		DefaultMaxRetries: c.DefaultMaxRetries, GlobalRateLimit: c.GlobalRateLimit,
		UsageEnabled: c.EnableUsage, CacheEnabled: c.EnableCache, RequireAPIKey: c.RequireAPIKey,
		LogLevel: c.LogLevel, AuthenticationScope: "HTTP api and proxy routes",
	}, nil
}
