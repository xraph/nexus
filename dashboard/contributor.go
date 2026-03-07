package dashboard

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contributor"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/dashboard/pages"
	"github.com/xraph/nexus/dashboard/widgets"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// GatewayConfig holds configuration values needed by the dashboard.
// This is a decoupled subset of extension.Config to avoid import cycles.
type GatewayConfig struct {
	BasePath          string
	DefaultTimeout    time.Duration
	DefaultMaxRetries int
	GlobalRateLimit   int
	LogLevel          string
	EnableUsage       *bool
	EnableCache       bool
}

// Ensure Contributor implements the required interface at compile time.
var _ contributor.LocalContributor = (*Contributor)(nil)

// Contributor implements the dashboard LocalContributor interface for the
// nexus extension. It renders pages, widgets, and settings using templ
// components and ForgeUI.
type Contributor struct {
	manifest *contributor.Manifest
	gw       *nexus.Gateway
	config   GatewayConfig
}

// New creates a new nexus dashboard contributor.
func New(manifest *contributor.Manifest, gw *nexus.Gateway, config GatewayConfig) *Contributor {
	return &Contributor{
		manifest: manifest,
		gw:       gw,
		config:   config,
	}
}

// Manifest returns the contributor manifest.
func (c *Contributor) Manifest() *contributor.Manifest { return c.manifest }

// RenderPage renders a page for the given route.
func (c *Contributor) RenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error) {
	switch route {
	case "/", "":
		return c.renderOverview(ctx)
	case "/tenants":
		return c.renderTenants(ctx, params)
	case "/tenants/detail":
		return c.renderTenantDetail(ctx, params)
	case "/tenants/create":
		return c.renderTenantCreate(ctx, params)
	case "/tenants/edit":
		return c.renderTenantEdit(ctx, params)
	case "/keys":
		return c.renderKeys(ctx, params)
	case "/keys/detail":
		return c.renderKeyDetail(ctx, params)
	case "/keys/create":
		return c.renderKeyCreate(ctx, params)
	case "/usage":
		return c.renderUsage(ctx, params)
	case "/usage/records":
		return c.renderUsageRecords(ctx, params)
	case "/models":
		return c.renderModels(ctx)
	case "/settings":
		return c.renderSettings(ctx)
	default:
		return nil, contributor.ErrPageNotFound
	}
}

// RenderWidget renders a widget by ID.
func (c *Contributor) RenderWidget(ctx context.Context, widgetID string) (templ.Component, error) {
	switch widgetID {
	case "nexus-stats":
		return c.renderStatsWidget(ctx)
	case "nexus-monthly-spend":
		return c.renderMonthlySpendWidget(ctx)
	case "nexus-recent-activity":
		return c.renderRecentActivityWidget(ctx)
	default:
		return nil, contributor.ErrWidgetNotFound
	}
}

// RenderSettings renders a settings panel by ID.
func (c *Contributor) RenderSettings(ctx context.Context, settingID string) (templ.Component, error) {
	switch settingID {
	case "nexus-config":
		return c.renderSettingsPanel(ctx)
	default:
		return nil, contributor.ErrSettingNotFound
	}
}

// ─── Page Renderers ──────────────────────────────────────────────────────────

func (c *Contributor) renderOverview(ctx context.Context) (templ.Component, error) {
	summary, _ := fetchUsageSummary(ctx, c.gw, "", "month")

	stats := pages.OverviewStats{
		TenantCount:   fetchTenantCount(ctx, c.gw),
		ActiveKeys:    fetchActiveKeyCount(ctx, c.gw),
		MonthlySpend:  fetchMonthlySpend(ctx, c.gw, ""),
		ProviderCount: 0,
	}

	if summary != nil {
		stats.TotalRequests = summary.TotalRequests
		stats.CacheHitRate = summary.CacheHitRate
	}

	if c.gw != nil && c.gw.Providers() != nil {
		stats.ProviderCount = c.gw.Providers().Count()
	}

	recentUsage, _ := fetchRecentUsage(ctx, c.gw, 10)

	return pages.OverviewPage(stats, recentUsage), nil
}

func (c *Contributor) renderTenants(ctx context.Context, params contributor.Params) (templ.Component, error) {
	opts := &tenant.ListOptions{Limit: 100}
	search := params.QueryParams["search"]
	if status := params.QueryParams["status"]; status != "" {
		opts.Status = status
	}

	tenants, total, err := fetchTenants(ctx, c.gw, opts)
	if err != nil {
		tenants = nil
		total = 0
	}

	// Client-side name search filter.
	if search != "" {
		var filtered []*tenant.Tenant
		lowerSearch := strings.ToLower(search)
		for _, t := range tenants {
			if strings.Contains(strings.ToLower(t.Name), lowerSearch) ||
				strings.Contains(strings.ToLower(t.Slug), lowerSearch) {
				filtered = append(filtered, t)
			}
		}
		tenants = filtered
		total = len(filtered)
	}

	return pages.TenantsPage(pages.TenantsPageData{
		Tenants: tenants,
		Total:   total,
		Search:  search,
	}), nil
}

func (c *Contributor) renderTenantDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	tenantID := params.QueryParams["id"]
	if tenantID == "" {
		tenantID = params.PathParams["id"]
	}
	if tenantID == "" {
		return nil, contributor.ErrPageNotFound
	}

	// Handle actions.
	if action := params.QueryParams["action"]; action != "" {
		switch action {
		case "enable":
			_ = c.gw.Tenants().SetStatus(ctx, tenantID, tenant.StatusActive)
		case "disable":
			_ = c.gw.Tenants().SetStatus(ctx, tenantID, tenant.StatusDisabled)
		case "suspend":
			_ = c.gw.Tenants().SetStatus(ctx, tenantID, tenant.StatusSuspended)
		}
	}

	t, err := fetchTenant(ctx, c.gw, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve tenant: %w", err)
	}

	keys, _ := fetchKeys(ctx, c.gw, tenantID)
	summary, _ := fetchUsageSummary(ctx, c.gw, tenantID, "month")

	return pages.TenantDetailPage(pages.TenantDetailData{
		Tenant:  t,
		Keys:    keys,
		Summary: summary,
	}), nil
}

func (c *Contributor) renderTenantCreate(ctx context.Context, params contributor.Params) (templ.Component, error) {
	// Handle form submission.
	if name := params.FormData["name"]; name != "" {
		input := &tenant.CreateInput{
			Name: name,
			Slug: params.FormData["slug"],
		}

		// Parse quota.
		quota := &tenant.Quota{}
		if v := params.FormData["rpm"]; v != "" {
			quota.RPM, _ = strconv.Atoi(v)
		}
		if v := params.FormData["tpm"]; v != "" {
			quota.TPM, _ = strconv.Atoi(v)
		}
		if v := params.FormData["daily_requests"]; v != "" {
			quota.DailyRequests, _ = strconv.Atoi(v)
		}
		if v := params.FormData["monthly_budget_usd"]; v != "" {
			f, _ := strconv.ParseFloat(v, 64)
			quota.MonthlyBudgetUSD = f
		}
		if v := params.FormData["max_tokens_per_req"]; v != "" {
			quota.MaxTokensPerReq, _ = strconv.Atoi(v)
		}
		input.Quota = quota

		// Parse config.
		config := &tenant.Config{
			DefaultModel:    params.FormData["default_model"],
			RoutingStrategy: params.FormData["routing_strategy"],
			GuardrailPolicy: params.FormData["guardrail_policy"],
		}
		if am := params.FormData["allowed_models"]; am != "" {
			config.AllowedModels = splitAndTrim(am)
		}
		if bm := params.FormData["blocked_models"]; bm != "" {
			config.BlockedModels = splitAndTrim(bm)
		}
		input.Config = config

		created, err := c.gw.Tenants().Create(ctx, input)
		if err != nil {
			return pages.TenantFormPage(pages.TenantFormData{
				IsEdit: false,
				Error:  err.Error(),
			}), nil
		}

		// Redirect to detail page.
		keys, _ := fetchKeys(ctx, c.gw, created.ID.String())
		return pages.TenantDetailPage(pages.TenantDetailData{
			Tenant: created,
			Keys:   keys,
		}), nil
	}

	return pages.TenantFormPage(pages.TenantFormData{IsEdit: false}), nil
}

func (c *Contributor) renderTenantEdit(ctx context.Context, params contributor.Params) (templ.Component, error) {
	tenantID := params.QueryParams["id"]
	if tenantID == "" {
		return nil, contributor.ErrPageNotFound
	}

	t, err := fetchTenant(ctx, c.gw, tenantID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve tenant: %w", err)
	}

	// Handle form submission.
	if name := params.FormData["name"]; name != "" {
		input := &tenant.UpdateInput{
			Name: &name,
		}

		quota := &tenant.Quota{}
		if v := params.FormData["rpm"]; v != "" {
			quota.RPM, _ = strconv.Atoi(v)
		}
		if v := params.FormData["tpm"]; v != "" {
			quota.TPM, _ = strconv.Atoi(v)
		}
		if v := params.FormData["daily_requests"]; v != "" {
			quota.DailyRequests, _ = strconv.Atoi(v)
		}
		if v := params.FormData["monthly_budget_usd"]; v != "" {
			f, _ := strconv.ParseFloat(v, 64)
			quota.MonthlyBudgetUSD = f
		}
		if v := params.FormData["max_tokens_per_req"]; v != "" {
			quota.MaxTokensPerReq, _ = strconv.Atoi(v)
		}
		input.Quota = quota

		config := &tenant.Config{
			DefaultModel:    params.FormData["default_model"],
			RoutingStrategy: params.FormData["routing_strategy"],
			GuardrailPolicy: params.FormData["guardrail_policy"],
		}
		if am := params.FormData["allowed_models"]; am != "" {
			config.AllowedModels = splitAndTrim(am)
		}
		if bm := params.FormData["blocked_models"]; bm != "" {
			config.BlockedModels = splitAndTrim(bm)
		}
		input.Config = config

		updated, err := c.gw.Tenants().Update(ctx, tenantID, input)
		if err != nil {
			return pages.TenantFormPage(pages.TenantFormData{
				Tenant: t,
				IsEdit: true,
				Error:  err.Error(),
			}), nil
		}

		keys, _ := fetchKeys(ctx, c.gw, updated.ID.String())
		summary, _ := fetchUsageSummary(ctx, c.gw, updated.ID.String(), "month")
		return pages.TenantDetailPage(pages.TenantDetailData{
			Tenant:  updated,
			Keys:    keys,
			Summary: summary,
		}), nil
	}

	return pages.TenantFormPage(pages.TenantFormData{
		Tenant: t,
		IsEdit: true,
	}), nil
}

func (c *Contributor) renderKeys(ctx context.Context, params contributor.Params) (templ.Component, error) {
	tenantIDFilter := params.QueryParams["tenant_id"]

	var keys []*key.APIKey
	if tenantIDFilter != "" {
		keys, _ = fetchKeys(ctx, c.gw, tenantIDFilter)
	} else {
		keys = fetchAllKeys(ctx, c.gw)
	}

	return pages.KeysPage(pages.KeysPageData{
		Keys:     keys,
		TenantID: tenantIDFilter,
		Total:    len(keys),
	}), nil
}

func (c *Contributor) renderKeyDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	keyID := params.QueryParams["id"]
	if keyID == "" {
		keyID = params.PathParams["id"]
	}
	if keyID == "" {
		return nil, contributor.ErrPageNotFound
	}

	var newRawKey string

	// Handle actions.
	if action := params.QueryParams["action"]; action != "" {
		switch action {
		case "revoke":
			_ = c.gw.Keys().Revoke(ctx, keyID)
		case "rotate":
			newKey, rawKey, err := c.gw.Keys().Rotate(ctx, keyID)
			if err == nil {
				newRawKey = rawKey
				keyID = newKey.ID.String()
			}
		}
	}

	k, err := fetchKeyByID(ctx, c.gw, keyID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve key: %w", err)
	}

	// Fetch usage records for this key.
	records, _, _ := fetchUsageRecords(ctx, c.gw, &usage.QueryOptions{
		TenantID: k.TenantID.String(),
		Limit:    20,
	})

	return pages.KeyDetailPage(pages.KeyDetailData{
		Key:       k,
		Records:   records,
		NewRawKey: newRawKey,
	}), nil
}

func (c *Contributor) renderKeyCreate(ctx context.Context, params contributor.Params) (templ.Component, error) {
	// Get tenants for dropdown.
	tenants, _, _ := fetchTenants(ctx, c.gw, &tenant.ListOptions{Limit: 1000})

	// Handle form submission.
	if tenantID := params.FormData["tenant_id"]; tenantID != "" {
		name := params.FormData["name"]
		if name == "" {
			return pages.KeyFormPage(pages.KeyFormData{
				Tenants: tenants,
				Error:   "Name is required.",
			}), nil
		}

		input := &key.CreateInput{
			TenantID: tenantID,
			Name:     name,
		}

		// Parse scopes from multi-value form field.
		if scopesStr := params.FormData["scopes"]; scopesStr != "" {
			input.Scopes = splitAndTrim(scopesStr)
		}

		created, rawKey, err := c.gw.Keys().Create(ctx, input)
		if err != nil {
			return pages.KeyFormPage(pages.KeyFormData{
				Tenants: tenants,
				Error:   err.Error(),
			}), nil
		}

		return pages.KeyFormPage(pages.KeyFormData{
			Tenants:    tenants,
			CreatedKey: created,
			RawKey:     rawKey,
		}), nil
	}

	return pages.KeyFormPage(pages.KeyFormData{
		Tenants: tenants,
	}), nil
}

func (c *Contributor) renderUsage(ctx context.Context, params contributor.Params) (templ.Component, error) {
	period := params.QueryParams["period"]
	if period == "" {
		period = "month"
	}
	tenantID := params.QueryParams["tenant_id"]

	summary, _ := fetchUsageSummary(ctx, c.gw, tenantID, period)

	return pages.UsagePage(pages.UsagePageData{
		Summary:      summary,
		MonthlySpend: fetchMonthlySpend(ctx, c.gw, tenantID),
		DailyReqs:    fetchDailyRequests(ctx, c.gw, tenantID),
		Period:       period,
		TenantID:     tenantID,
	}), nil
}

func (c *Contributor) renderUsageRecords(ctx context.Context, params contributor.Params) (templ.Component, error) {
	opts := &usage.QueryOptions{Limit: 50}
	tenantID := params.QueryParams["tenant_id"]
	providerFilter := params.QueryParams["provider"]
	modelFilter := params.QueryParams["model"]

	if tenantID != "" {
		opts.TenantID = tenantID
	}
	if providerFilter != "" {
		opts.Provider = providerFilter
	}
	if modelFilter != "" {
		opts.Model = modelFilter
	}

	records, total, _ := fetchUsageRecords(ctx, c.gw, opts)

	return pages.UsageRecordsPage(pages.UsageRecordsPageData{
		Records:  records,
		Total:    total,
		TenantID: tenantID,
		Provider: providerFilter,
		Model:    modelFilter,
	}), nil
}

func (c *Contributor) renderModels(ctx context.Context) (templ.Component, error) {
	models, _ := fetchModels(ctx, c.gw)
	providers := fetchProviderInfos(ctx, c.gw)

	return pages.ModelsPage(pages.ModelsPageData{
		Models:    models,
		Providers: providers,
	}), nil
}

func (c *Contributor) renderSettings(_ context.Context) (templ.Component, error) {
	data := pages.SettingsData{
		BasePath:   c.config.BasePath,
		MaxRetries: c.config.DefaultMaxRetries,
		LogLevel:   c.config.LogLevel,
	}

	if c.config.DefaultTimeout > 0 {
		data.DefaultTimeout = c.config.DefaultTimeout
	}
	if c.config.GlobalRateLimit > 0 {
		data.RateLimit = c.config.GlobalRateLimit
	}
	if c.config.EnableUsage != nil {
		data.EnableUsage = *c.config.EnableUsage
	}
	data.EnableCache = c.config.EnableCache

	if c.gw != nil {
		if c.gw.Providers() != nil {
			data.ProviderCount = c.gw.Providers().Count()
		}
		if c.gw.Extensions() != nil {
			data.ExtensionCount = c.gw.Extensions().Count()
		}
	}

	return pages.SettingsPage(data), nil
}

// ─── Widget Renderers ────────────────────────────────────────────────────────

func (c *Contributor) renderStatsWidget(ctx context.Context) (templ.Component, error) {
	summary, _ := fetchUsageSummary(ctx, c.gw, "", "month")

	data := widgets.StatsData{
		TenantCount:  fetchTenantCount(ctx, c.gw),
		ActiveKeys:   fetchActiveKeyCount(ctx, c.gw),
		MonthlySpend: fetchMonthlySpend(ctx, c.gw, ""),
	}
	if summary != nil {
		data.TotalRequests = summary.TotalRequests
	}

	return widgets.StatsWidget(data), nil
}

func (c *Contributor) renderMonthlySpendWidget(ctx context.Context) (templ.Component, error) {
	return widgets.MonthlySpendWidget(widgets.MonthlySpendData{
		Spend: fetchMonthlySpend(ctx, c.gw, ""),
	}), nil
}

func (c *Contributor) renderRecentActivityWidget(ctx context.Context) (templ.Component, error) {
	records, _ := fetchRecentUsage(ctx, c.gw, 5)
	return widgets.RecentActivityWidget(records), nil
}

// ─── Settings Renderer ───────────────────────────────────────────────────────

func (c *Contributor) renderSettingsPanel(ctx context.Context) (templ.Component, error) {
	return c.renderSettings(ctx)
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
