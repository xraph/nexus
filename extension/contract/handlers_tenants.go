package contract

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
	"github.com/xraph/nexus/money"
	"github.com/xraph/nexus/tenant"
)

type idRequest struct {
	ID string `json:"id"`
}
type tenantsListRequest struct {
	Status string `json:"status"`
	Search string `json:"search"`
	Cursor string `json:"cursor"`
	Limit  int    `json:"limit"`
}
type tenantsListResponse struct {
	Items      []tenantRow `json:"items"`
	NextCursor string      `json:"nextCursor"`
}

func validTenantStatus(s string) bool {
	return s == string(tenant.StatusActive) || s == string(tenant.StatusDisabled) || s == string(tenant.StatusSuspended)
}

func tenantsList(ctx context.Context, gw *nexus.Gateway, in tenantsListRequest) (tenantsListResponse, error) {
	if in.Status != "" && !validTenantStatus(in.Status) {
		return tenantsListResponse{}, badRequest("invalid tenant status")
	}
	page, err := gw.Tenants().List(ctx, &tenant.ListOptions{Status: in.Status, Search: in.Search, Cursor: in.Cursor, Limit: in.Limit})
	if err != nil {
		return tenantsListResponse{}, err
	}
	out := tenantsListResponse{Items: make([]tenantRow, 0, len(page.Items)), NextCursor: page.NextCursor}
	for _, t := range page.Items {
		row, err := tenantWithUsage(ctx, gw, t)
		if err != nil {
			return tenantsListResponse{}, err
		}
		out.Items = append(out.Items, row)
	}
	return out, nil
}

func getTenant(ctx context.Context, gw *nexus.Gateway, tid string) (*tenant.Tenant, error) {
	if _, err := id.ParseTenantID(tid); err != nil {
		return nil, badRequest("id must be a tenant ID")
	}
	return gw.Tenants().Get(ctx, tid)
}

func tenantsGet(ctx context.Context, gw *nexus.Gateway, in idRequest) (tenantRow, error) {
	t, err := getTenant(ctx, gw, in.ID)
	if err != nil {
		return tenantRow{}, err
	}
	return tenantWithUsage(ctx, gw, t)
}

func tenantWithUsage(ctx context.Context, gw *nexus.Gateway, t *tenant.Tenant) (tenantRow, error) {
	out := projectTenant(t)
	out.UsageEnabled = gw.Config().EnableUsage
	if !out.UsageEnabled {
		return out, nil
	}
	spend, err := gw.Usage().MonthlySpend(ctx, t.ID.String())
	if err != nil {
		return tenantRow{}, err
	}
	count, err := gw.Usage().DailyRequests(ctx, t.ID.String())
	if err != nil {
		return tenantRow{}, err
	}
	value := spend.String()
	out.MonthSpendUSD = &value
	out.RequestsToday = &count
	return out, nil
}

type quotaPatch struct {
	RPM                 *int    `json:"rpm"`
	TPM                 *int    `json:"tpm"`
	DailyRequests       *int    `json:"dailyRequests"`
	MonthlyBudgetUSD    *string `json:"monthlyBudgetUsd"`
	MaxTokensPerReq     *int    `json:"maxTokensPerReq"`
	MaxStreamDurationMS *int64  `json:"maxStreamDurationMs"`
	MaxStreamTokens     *int    `json:"maxStreamTokens"`
}

func (p *quotaPatch) apply(q tenant.Quota) (tenant.Quota, error) {
	if p == nil {
		return q, nil
	}
	for _, pair := range []struct {
		src *int
		dst *int
	}{{p.RPM, &q.RPM}, {p.TPM, &q.TPM}, {p.DailyRequests, &q.DailyRequests}, {p.MaxTokensPerReq, &q.MaxTokensPerReq}, {p.MaxStreamTokens, &q.MaxStreamTokens}} {
		if pair.src != nil {
			if *pair.src < 0 {
				return q, badRequest("quota limits must be non-negative")
			}
			*pair.dst = *pair.src
		}
	}
	if p.MonthlyBudgetUSD != nil {
		budget, err := money.Parse(*p.MonthlyBudgetUSD)
		if err != nil || budget.IsNegative() {
			return q, badRequest("monthlyBudgetUsd must be a non-negative decimal string")
		}
		q.MonthlyBudgetUSD = budget
	}
	if p.MaxStreamDurationMS != nil {
		n := *p.MaxStreamDurationMS
		if n < 0 || n > math.MaxInt64/int64(time.Millisecond) {
			return q, badRequest("maxStreamDurationMs is outside the supported range")
		}
		q.MaxStreamDuration = time.Duration(n) * time.Millisecond
	}
	return q, nil
}

type configPatch struct {
	AllowedModels   *[]string          `json:"allowedModels"`
	BlockedModels   *[]string          `json:"blockedModels"`
	DefaultModel    *string            `json:"defaultModel"`
	RoutingStrategy *string            `json:"routingStrategy"`
	GuardrailPolicy *string            `json:"guardrailPolicy"`
	CacheEnabled    json.RawMessage    `json:"cacheEnabled"`
	Metadata        *map[string]string `json:"metadata"`
}

func (p *configPatch) apply(c tenant.Config) (tenant.Config, error) {
	if p == nil {
		return c, nil
	}
	if p.AllowedModels != nil {
		c.AllowedModels = *p.AllowedModels
	}
	if p.BlockedModels != nil {
		c.BlockedModels = *p.BlockedModels
	}
	if p.DefaultModel != nil {
		c.DefaultModel = strings.TrimSpace(*p.DefaultModel)
	}
	if p.RoutingStrategy != nil {
		c.RoutingStrategy = *p.RoutingStrategy
	}
	if p.GuardrailPolicy != nil {
		c.GuardrailPolicy = *p.GuardrailPolicy
	}
	if len(p.CacheEnabled) > 0 {
		var v *bool
		if err := json.Unmarshal(p.CacheEnabled, &v); err != nil {
			return c, badRequest("cacheEnabled must be true, false or null")
		}
		c.CacheEnabled = v
	}
	if p.Metadata != nil {
		c.Metadata = *p.Metadata
	}
	return c, nil
}

type tenantCreateRequest struct {
	Name     string            `json:"name"`
	Slug     string            `json:"slug"`
	Quota    *quotaPatch       `json:"quota"`
	Config   *configPatch      `json:"config"`
	Metadata map[string]string `json:"metadata"`
}

func tenantsCreate(ctx context.Context, gw *nexus.Gateway, in tenantCreateRequest) (tenantRow, error) {
	name, slug := strings.TrimSpace(in.Name), strings.TrimSpace(in.Slug)
	if name == "" || slug == "" {
		return tenantRow{}, badRequest("name and slug are required")
	}
	q, err := in.Quota.apply(tenant.Quota{})
	if err != nil {
		return tenantRow{}, err
	}
	c, err := in.Config.apply(tenant.Config{})
	if err != nil {
		return tenantRow{}, err
	}
	t, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: name, Slug: slug, Quota: &q, Config: &c, Metadata: in.Metadata})
	if err != nil {
		return tenantRow{}, err
	}
	// Writes return the committed entity without a second fallible read.
	out := projectTenant(t)
	out.UsageEnabled = gw.Config().EnableUsage
	return out, nil
}

type tenantUpdateRequest struct {
	ID       string             `json:"id"`
	Name     *string            `json:"name"`
	Quota    *quotaPatch        `json:"quota"`
	Config   *configPatch       `json:"config"`
	Metadata *map[string]string `json:"metadata"`
}

func tenantsUpdate(ctx context.Context, gw *nexus.Gateway, in tenantUpdateRequest) (tenantRow, error) {
	t, err := getTenant(ctx, gw, in.ID)
	if err != nil {
		return tenantRow{}, err
	}
	if in.Name != nil {
		name := strings.TrimSpace(*in.Name)
		if name == "" {
			return tenantRow{}, badRequest("name is required")
		}
		in.Name = &name
	}
	q, err := in.Quota.apply(t.Quota)
	if err != nil {
		return tenantRow{}, err
	}
	c, err := in.Config.apply(t.Config)
	if err != nil {
		return tenantRow{}, err
	}
	u := &tenant.UpdateInput{Name: in.Name}
	if in.Quota != nil {
		u.Quota = &q
	}
	if in.Config != nil {
		u.Config = &c
	}
	if in.Metadata != nil {
		u.Metadata = *in.Metadata
	}
	t, err = gw.Tenants().Update(ctx, in.ID, u)
	if err != nil {
		return tenantRow{}, err
	}
	out := projectTenant(t)
	out.UsageEnabled = gw.Config().EnableUsage
	return out, nil
}

type tenantStatusRequest struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

func tenantsSetStatus(ctx context.Context, gw *nexus.Gateway, in tenantStatusRequest) (tenantRow, error) {
	if !validTenantStatus(in.Status) {
		return tenantRow{}, badRequest("invalid tenant status")
	}
	t, err := getTenant(ctx, gw, in.ID)
	if err != nil {
		return tenantRow{}, err
	}
	if err := gw.Tenants().SetStatus(ctx, in.ID, tenant.Status(in.Status)); err != nil {
		return tenantRow{}, err
	}
	t.Status = tenant.Status(in.Status)
	// SetStatus's service clock owns UpdatedAt, so this response leaves that
	// timestamp absent until the invalidated detail query reads it again.
	t.UpdatedAt = time.Time{}
	out := projectTenant(t)
	out.UsageEnabled = gw.Config().EnableUsage
	return out, nil
}
