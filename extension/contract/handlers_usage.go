package contract

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/usage"
)

type usageSummaryRequest struct {
	TenantID json.RawMessage `json:"tenantId"`
	Period   string          `json:"period"`
}
type aggregateRow struct {
	Name     string `json:"name"`
	Requests int    `json:"requests"`
	Tokens   int    `json:"tokens"`
	CostUSD  string `json:"costUsd"`
	Unpriced int    `json:"unpriced"`
	cost     money.USD
}
type usageSummaryResponse struct {
	TenantID         *string               `json:"tenantId"`
	Period           string                `json:"period"`
	UsageEnabled     bool                  `json:"usageEnabled"`
	TotalRequests    *int                  `json:"totalRequests"`
	TotalTokens      *int                  `json:"totalTokens"`
	TotalCostUSD     *string               `json:"totalCostUsd"`
	UnpricedRequests *int                  `json:"unpricedRequests"`
	CacheHitRate     *float64              `json:"cacheHitRate"`
	AvgLatencyMS     *int64                `json:"avgLatencyMs"`
	ByOutcome        map[usage.Outcome]int `json:"byOutcome"`
	ByProvider       []aggregateRow        `json:"byProvider"`
	ByModel          []aggregateRow        `json:"byModel"`
}

func ptr[T any](v T) *T { return &v }
func optionalString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
func outcomeCounts(in map[usage.Outcome]int) map[usage.Outcome]int {
	out := map[usage.Outcome]int{usage.OutcomeOK: 0, usage.OutcomeCached: 0, usage.OutcomeBlocked: 0, usage.OutcomeRefused: 0, usage.OutcomeError: 0}
	for k, v := range in {
		out[k] = v
	}
	return out
}
func sortAggregates(rows []aggregateRow) {
	slices.SortFunc(rows, func(a, b aggregateRow) int {
		if n := b.cost.Cmp(a.cost); n != 0 {
			return n
		}
		return strings.Compare(a.Name, b.Name)
	})
}
func usageSummary(ctx context.Context, gw *nexus.Gateway, in usageSummaryRequest) (usageSummaryResponse, error) {
	tid, err := tenantScope(ctx, gw, in.TenantID)
	if err != nil {
		return usageSummaryResponse{}, err
	}
	if _, periodErr := usage.PeriodStart(in.Period, time.Now()); periodErr != nil {
		return usageSummaryResponse{}, periodErr
	}
	out := usageSummaryResponse{TenantID: optionalString(tid), Period: in.Period, UsageEnabled: gw.Config().EnableUsage, ByProvider: []aggregateRow{}, ByModel: []aggregateRow{}}
	if !out.UsageEnabled {
		return out, nil
	}
	s, err := gw.Usage().Summary(ctx, tid, in.Period)
	if err != nil {
		return usageSummaryResponse{}, err
	}
	out.TotalRequests = ptr(s.TotalRequests)
	out.TotalTokens = ptr(s.TotalTokens)
	out.TotalCostUSD = ptr(s.TotalCostUSD.String())
	out.UnpricedRequests = ptr(s.UnpricedRequests)
	out.CacheHitRate = ptr(s.CacheHitRate)
	out.AvgLatencyMS = ptr(s.AvgLatency.Milliseconds())
	out.ByOutcome = outcomeCounts(s.ByOutcome)
	for name, v := range s.ByProvider {
		out.ByProvider = append(out.ByProvider, aggregateRow{Name: name, Requests: v.Requests, Tokens: v.Tokens, CostUSD: v.CostUSD.String(), Unpriced: v.Unpriced, cost: v.CostUSD})
	}
	for name, v := range s.ByModel {
		out.ByModel = append(out.ByModel, aggregateRow{Name: name, Requests: v.Requests, Tokens: v.Tokens, CostUSD: v.CostUSD.String(), Unpriced: v.Unpriced, cost: v.CostUSD})
	}
	sortAggregates(out.ByProvider)
	sortAggregates(out.ByModel)
	return out, nil
}

type usageSeriesRequest struct {
	TenantID json.RawMessage `json:"tenantId"`
	Period   string          `json:"period"`
	Bucket   usage.Bucket    `json:"bucket"`
}
type seriesRow struct {
	Start    *string `json:"start"`
	Requests int     `json:"requests"`
	Tokens   int     `json:"tokens"`
	CostUSD  string  `json:"costUsd"`
	Unpriced int     `json:"unpriced"`
}
type usageSeriesResponse struct {
	UsageEnabled bool        `json:"usageEnabled"`
	Items        []seriesRow `json:"items"`
}

func usageSeries(ctx context.Context, gw *nexus.Gateway, in usageSeriesRequest) (usageSeriesResponse, error) {
	tid, err := tenantScope(ctx, gw, in.TenantID)
	if err != nil {
		return usageSeriesResponse{}, err
	}
	now := time.Now().UTC()
	start, err := usage.PeriodStart(in.Period, now)
	if err != nil {
		return usageSeriesResponse{}, err
	}
	if in.Bucket != usage.BucketDay && in.Bucket != usage.BucketHour {
		return usageSeriesResponse{}, badRequest("bucket must be day or hour")
	}
	out := usageSeriesResponse{UsageEnabled: gw.Config().EnableUsage, Items: []seriesRow{}}
	if !out.UsageEnabled {
		return out, nil
	}
	points, err := gw.Usage().Series(ctx, &usage.SeriesOptions{TenantID: tid, Start: start, End: now, Bucket: in.Bucket})
	if err != nil {
		return usageSeriesResponse{}, err
	}
	for _, p := range points {
		out.Items = append(out.Items, seriesRow{Start: formatTime(p.Start), Requests: p.Requests, Tokens: p.Tokens, CostUSD: p.CostUSD.String(), Unpriced: p.Unpriced})
	}
	return out, nil
}

type usageRecordsRequest struct {
	TenantID json.RawMessage `json:"tenantId"`
	KeyID    json.RawMessage `json:"keyId"`
	Provider string          `json:"provider"`
	Model    string          `json:"model"`
	Outcome  usage.Outcome   `json:"outcome"`
	From     *string         `json:"from"`
	To       *string         `json:"to"`
	Cursor   string          `json:"cursor"`
	Limit    int             `json:"limit"`
}
type usageRecordRow struct {
	ID               string              `json:"id"`
	TenantID         *string             `json:"tenantId"`
	KeyID            *string             `json:"keyId"`
	RequestID        *string             `json:"requestId"`
	Provider         string              `json:"provider"`
	Model            string              `json:"model"`
	PromptTokens     int                 `json:"promptTokens"`
	CompletionTokens int                 `json:"completionTokens"`
	TotalTokens      int                 `json:"totalTokens"`
	CostUSD          *string             `json:"costUsd"`
	PricingStatus    usage.PricingStatus `json:"pricingStatus"`
	Outcome          usage.Outcome       `json:"outcome"`
	BlockedBy        string              `json:"blockedBy"`
	RefusalCode      string              `json:"refusalCode"`
	LatencyMS        int64               `json:"latencyMs"`
	Cached           bool                `json:"cached"`
	StatusCode       int                 `json:"statusCode"`
	CreatedAt        *string             `json:"createdAt"`
}
type usageRecordsResponse struct {
	UsageEnabled bool             `json:"usageEnabled"`
	Items        []usageRecordRow `json:"items"`
	NextCursor   string           `json:"nextCursor"`
}

func nullableID(v id.ID) *string {
	if v.IsNil() {
		return nil
	}
	return ptr(v.String())
}
func projectRecord(r *usage.Record) usageRecordRow {
	out := usageRecordRow{ID: r.ID.String(), TenantID: nullableID(r.TenantID), KeyID: nullableID(r.KeyID), RequestID: nullableID(r.RequestID), Provider: r.Provider, Model: r.Model, PromptTokens: r.PromptTokens, CompletionTokens: r.CompletionTokens, TotalTokens: r.TotalTokens, PricingStatus: r.PricingStatus, Outcome: r.Outcome, BlockedBy: r.BlockedBy, RefusalCode: r.RefusalCode, LatencyMS: r.Latency.Milliseconds(), Cached: r.Cached, StatusCode: r.StatusCode, CreatedAt: formatTime(r.CreatedAt)}
	if r.CostUSD != nil {
		out.CostUSD = ptr(r.CostUSD.String())
	}
	return out
}
func parseBound(raw *string) (time.Time, error) {
	if raw == nil {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339Nano, *raw)
	if err != nil {
		return time.Time{}, badRequest("time bounds must be RFC3339 timestamps")
	}
	return t.UTC(), nil
}
func usageRecords(ctx context.Context, gw *nexus.Gateway, in usageRecordsRequest) (usageRecordsResponse, error) {
	tid, err := tenantScope(ctx, gw, in.TenantID)
	if err != nil {
		return usageRecordsResponse{}, err
	}
	opts := &usage.QueryOptions{TenantID: tid, Provider: in.Provider, Model: in.Model, Outcome: in.Outcome, Cursor: in.Cursor, Limit: in.Limit}
	if len(in.KeyID) > 0 {
		var kid string
		if json.Unmarshal(in.KeyID, &kid) != nil {
			return usageRecordsResponse{}, badRequest("keyId must be a key ID")
		}
		if _, parseErr := id.ParseKeyID(kid); parseErr != nil {
			return usageRecordsResponse{}, badRequest("keyId must be a key ID")
		}
		k, keyErr := gw.Keys().Get(ctx, kid)
		if keyErr != nil {
			return usageRecordsResponse{}, keyErr
		}
		if tid != "" && k.TenantID.String() != tid {
			return usageRecordsResponse{}, badRequest("key does not belong to this tenant")
		}
		opts.KeyID = kid
	}
	switch in.Outcome {
	case "", usage.OutcomeOK, usage.OutcomeCached, usage.OutcomeRefused, usage.OutcomeBlocked, usage.OutcomeError:
	default:
		return usageRecordsResponse{}, badRequest("invalid outcome")
	}
	if cursorErr := paging.CheckCursor(in.Cursor, id.PrefixUsage); cursorErr != nil {
		return usageRecordsResponse{}, cursorErr
	}
	opts.StartTime, err = parseBound(in.From)
	if err != nil {
		return usageRecordsResponse{}, err
	}
	opts.EndTime, err = parseBound(in.To)
	if err != nil {
		return usageRecordsResponse{}, err
	}
	if in.From != nil && in.To != nil && !opts.EndTime.After(opts.StartTime) {
		return usageRecordsResponse{}, badRequest("to must be after from")
	}
	out := usageRecordsResponse{UsageEnabled: gw.Config().EnableUsage, Items: []usageRecordRow{}}
	if !out.UsageEnabled {
		return out, nil
	}
	page, err := gw.Usage().Query(ctx, opts)
	if err != nil {
		return usageRecordsResponse{}, err
	}
	out.NextCursor = page.NextCursor
	for _, r := range page.Items {
		out.Items = append(out.Items, projectRecord(r))
	}
	return out, nil
}
