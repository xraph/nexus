// Package usage defines usage tracking types and service.
package usage

import (
	"context"
	"time"

	"github.com/xraph/nexus/id"
)

// Record captures a single API call's usage.
type Record struct {
	ID               id.UsageID    `json:"id"`
	TenantID         id.TenantID   `json:"tenant_id"`
	KeyID            id.KeyID      `json:"key_id"`
	RequestID        id.RequestID  `json:"request_id"`
	Provider         string        `json:"provider"`
	Model            string        `json:"model"`
	PromptTokens     int           `json:"prompt_tokens"`
	CompletionTokens int           `json:"completion_tokens"`
	TotalTokens      int           `json:"total_tokens"`
	CostUSD          float64       `json:"cost_usd"`
	Latency          time.Duration `json:"latency"`
	Cached           bool          `json:"cached"`
	StatusCode       int           `json:"status_code"`
	CreatedAt        time.Time     `json:"created_at"`
}

// Summary aggregates usage over a period.
type Summary struct {
	TenantID      string                    `json:"tenant_id"`
	Period        string                    `json:"period"` // "day", "week", "month"
	TotalRequests int                       `json:"total_requests"`
	TotalTokens   int                       `json:"total_tokens"`
	TotalCostUSD  float64                   `json:"total_cost_usd"`
	CacheHitRate  float64                   `json:"cache_hit_rate"`
	AvgLatency    time.Duration             `json:"avg_latency"`
	ByProvider    map[string]*ProviderUsage `json:"by_provider"`
	ByModel       map[string]*ModelUsage    `json:"by_model"`
}

// ProviderUsage is usage aggregated by provider.
type ProviderUsage struct {
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

// ModelUsage is usage aggregated by model.
type ModelUsage struct {
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	CostUSD  float64 `json:"cost_usd"`
}

// QueryOptions configures usage queries.
type QueryOptions struct {
	TenantID  string    `json:"tenant_id,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	StartTime time.Time `json:"start_time,omitempty"`
	EndTime   time.Time `json:"end_time,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	Offset    int       `json:"offset,omitempty"`
}

// Service tracks and queries usage data.
type Service interface {
	Record(ctx context.Context, rec *Record) error
	MonthlySpend(ctx context.Context, tenantID string) (float64, error)
	DailyRequests(ctx context.Context, tenantID string) (int, error)
	Summary(ctx context.Context, tenantID string, period string) (*Summary, error)
	Query(ctx context.Context, opts *QueryOptions) ([]*Record, int, error)
}

// Store is the persistence interface for usage records.
type Store interface {
	Insert(ctx context.Context, rec *Record) error
	MonthlySpend(ctx context.Context, tenantID string) (float64, error)
	DailyRequests(ctx context.Context, tenantID string) (int, error)
	Summary(ctx context.Context, tenantID string, period string) (*Summary, error)
	Query(ctx context.Context, opts *QueryOptions) ([]*Record, int, error)
}
