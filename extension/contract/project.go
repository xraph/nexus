package contract

import (
	"time"

	"github.com/xraph/nexus/tenant"
)

func formatTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	s := t.UTC().Format(time.RFC3339Nano)
	return &s
}

func nonNil[T any](v []T) []T {
	if v == nil {
		return []T{}
	}
	return v
}

type quotaView struct {
	RPM                 int    `json:"rpm"`
	TPM                 int    `json:"tpm"`
	DailyRequests       int    `json:"dailyRequests"`
	MonthlyBudgetUSD    string `json:"monthlyBudgetUsd"`
	MaxTokensPerReq     int    `json:"maxTokensPerReq"`
	MaxStreamDurationMS int64  `json:"maxStreamDurationMs"`
	MaxStreamTokens     int    `json:"maxStreamTokens"`
}

type tenantConfigView struct {
	AllowedModels           []string          `json:"allowedModels"`
	BlockedModels           []string          `json:"blockedModels"`
	DefaultModel            string            `json:"defaultModel"`
	RoutingStrategy         string            `json:"routingStrategy"`
	RoutingStrategyEnforced bool              `json:"routingStrategyEnforced"`
	GuardrailPolicy         string            `json:"guardrailPolicy"`
	GuardrailPolicyEnforced bool              `json:"guardrailPolicyEnforced"`
	CacheEnabled            *bool             `json:"cacheEnabled"`
	Metadata                map[string]string `json:"metadata"`
}

type tenantRow struct {
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Slug          string            `json:"slug"`
	Status        tenant.Status     `json:"status"`
	Quota         quotaView         `json:"quota"`
	Config        tenantConfigView  `json:"config"`
	Metadata      map[string]string `json:"metadata"`
	CreatedAt     *string           `json:"createdAt"`
	UpdatedAt     *string           `json:"updatedAt"`
	MonthSpendUSD *string           `json:"monthSpendUsd"`
	RequestsToday *int              `json:"requestsToday"`
	UsageEnabled  bool              `json:"usageEnabled"`
}

func projectTenant(t *tenant.Tenant) tenantRow {
	q, c := t.Quota, t.Config
	return tenantRow{
		ID: t.ID.String(), Name: t.Name, Slug: t.Slug, Status: t.Status,
		Quota:    quotaView{RPM: q.RPM, TPM: q.TPM, DailyRequests: q.DailyRequests, MonthlyBudgetUSD: q.MonthlyBudgetUSD.String(), MaxTokensPerReq: q.MaxTokensPerReq, MaxStreamDurationMS: q.MaxStreamDuration.Milliseconds(), MaxStreamTokens: q.MaxStreamTokens},
		Config:   tenantConfigView{AllowedModels: nonNil(c.AllowedModels), BlockedModels: nonNil(c.BlockedModels), DefaultModel: c.DefaultModel, RoutingStrategy: c.RoutingStrategy, GuardrailPolicy: c.GuardrailPolicy, CacheEnabled: c.CacheEnabled, Metadata: c.Metadata},
		Metadata: t.Metadata, CreatedAt: formatTime(t.CreatedAt), UpdatedAt: formatTime(t.UpdatedAt),
	}
}
