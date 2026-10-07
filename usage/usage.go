// Package usage defines usage tracking types and service.
package usage

import (
	"context"
	"time"

	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
)

// Record captures a single API call's usage.
type Record struct {
	ID id.UsageID `json:"id"`
	// TenantID, KeyID and RequestID are id.Nil when the request was not
	// attributed to them.
	TenantID         id.TenantID  `json:"tenant_id"`
	KeyID            id.KeyID     `json:"key_id"`
	RequestID        id.RequestID `json:"request_id"`
	Provider         string       `json:"provider"`
	Model            string       `json:"model"`
	PromptTokens     int          `json:"prompt_tokens"`
	CompletionTokens int          `json:"completion_tokens"`
	TotalTokens      int          `json:"total_tokens"`
	// CostUSD is nil when the request could not be priced. It is never $0
	// standing in for "unknown"; PricingStatus says why it is missing.
	CostUSD       *money.USD    `json:"cost_usd"`
	PricingStatus PricingStatus `json:"pricing_status"`
	Outcome       Outcome       `json:"outcome"`
	BlockedBy     string        `json:"blocked_by,omitempty"`
	RefusalCode   string        `json:"refusal_code,omitempty"`
	Latency       time.Duration `json:"latency"`
	Cached        bool          `json:"cached"`
	StatusCode    int           `json:"status_code"`
	CreatedAt     time.Time     `json:"created_at"`
}

// Summary aggregates usage over a period.
type Summary struct {
	// TenantID is "" when the summary covers every tenant, unattributed
	// requests included.
	TenantID      string `json:"tenant_id"`
	Period        string `json:"period"`
	TotalRequests int    `json:"total_requests"`
	TotalTokens   int    `json:"total_tokens"`
	// TotalCostUSD sums priced requests only. UnpricedRequests counts the
	// requests it leaves out because their cost is unknown.
	TotalCostUSD     money.USD                 `json:"total_cost_usd"`
	UnpricedRequests int                       `json:"unpriced_requests"`
	CacheHitRate     float64                   `json:"cache_hit_rate"` // share of requests served from cache, 0 to 1
	AvgLatency       time.Duration             `json:"avg_latency"`
	ByProvider       map[string]*ProviderUsage `json:"by_provider"`
	ByModel          map[string]*ModelUsage    `json:"by_model"`
	ByOutcome        map[Outcome]int           `json:"by_outcome"`
}

// ProviderUsage is usage aggregated by provider.
type ProviderUsage struct {
	Requests int       `json:"requests"`
	Tokens   int       `json:"tokens"`
	CostUSD  money.USD `json:"cost_usd"`
	Unpriced int       `json:"unpriced"`
}

// ModelUsage is usage aggregated by model.
type ModelUsage struct {
	Requests int       `json:"requests"`
	Tokens   int       `json:"tokens"`
	CostUSD  money.USD `json:"cost_usd"`
	Unpriced int       `json:"unpriced"`
}

// QueryOptions filters the request log. Every filter is applied by the
// store, never after reading a window, so an empty page means nothing
// matched. An empty TenantID means every tenant. EndTime is exclusive.
type QueryOptions struct {
	TenantID  string    `json:"tenant_id,omitempty"`
	KeyID     string    `json:"key_id,omitempty"`
	Provider  string    `json:"provider,omitempty"`
	Model     string    `json:"model,omitempty"`
	Outcome   Outcome   `json:"outcome,omitempty"`
	StartTime time.Time `json:"start_time,omitzero"`
	EndTime   time.Time `json:"end_time,omitzero"`
	Limit     int       `json:"limit,omitempty"`
	Cursor    string    `json:"cursor,omitempty"`
}

// QueryResult is one page of records. NextCursor is "" on the last page.
type QueryResult struct {
	Items      []*Record `json:"items"`
	NextCursor string    `json:"next_cursor"`
}

// Service tracks and queries usage data. Wherever a method takes a tenant
// id, "" means every tenant, including requests attributed to none.
type Service interface {
	Record(ctx context.Context, rec *Record) error
	MonthlySpend(ctx context.Context, tenantID string) (money.USD, error)
	DailyRequests(ctx context.Context, tenantID string) (int, error)
	Summary(ctx context.Context, tenantID string, period string) (*Summary, error)
	Query(ctx context.Context, opts *QueryOptions) (*QueryResult, error)
}

// Store is the persistence interface for usage records. Wherever a method
// takes a tenant id, "" means every tenant, including requests attributed
// to none. Summary returns ErrInvalidPeriod for a period other than day,
// week or month.
type Store interface {
	Insert(ctx context.Context, rec *Record) error
	MonthlySpend(ctx context.Context, tenantID string) (money.USD, error)
	DailyRequests(ctx context.Context, tenantID string) (int, error)
	Summary(ctx context.Context, tenantID string, period string) (*Summary, error)
	Query(ctx context.Context, opts *QueryOptions) (*QueryResult, error)
}
