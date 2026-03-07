package dashboard

import (
	"context"
	"fmt"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/dashboard/components"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

// --- Tenant fetchers ---

func fetchTenants(ctx context.Context, gw *nexus.Gateway, opts *tenant.ListOptions) ([]*tenant.Tenant, int, error) {
	if gw == nil || gw.Tenants() == nil {
		return nil, 0, nil
	}
	return gw.Tenants().List(ctx, opts)
}

func fetchTenant(ctx context.Context, gw *nexus.Gateway, id string) (*tenant.Tenant, error) {
	if gw == nil || gw.Tenants() == nil {
		return nil, fmt.Errorf("tenant service unavailable")
	}
	return gw.Tenants().Get(ctx, id)
}

func fetchTenantCount(ctx context.Context, gw *nexus.Gateway) int {
	if gw == nil || gw.Tenants() == nil {
		return 0
	}
	_, total, err := gw.Tenants().List(ctx, &tenant.ListOptions{Limit: 1})
	if err != nil {
		return 0
	}
	return total
}

// --- Key fetchers ---

func fetchKeys(ctx context.Context, gw *nexus.Gateway, tenantID string) ([]*key.APIKey, error) {
	if gw == nil || gw.Keys() == nil {
		return nil, nil
	}
	return gw.Keys().List(ctx, tenantID)
}

func fetchAllKeys(ctx context.Context, gw *nexus.Gateway) []*key.APIKey {
	if gw == nil || gw.Tenants() == nil || gw.Keys() == nil {
		return nil
	}
	tenants, _, err := gw.Tenants().List(ctx, &tenant.ListOptions{Limit: 1000})
	if err != nil {
		return nil
	}
	var all []*key.APIKey
	for _, t := range tenants {
		keys, err := gw.Keys().List(ctx, t.ID.String())
		if err == nil {
			all = append(all, keys...)
		}
	}
	return all
}

func fetchActiveKeyCount(ctx context.Context, gw *nexus.Gateway) int {
	keys := fetchAllKeys(ctx, gw)
	count := 0
	for _, k := range keys {
		if k.Status == key.KeyActive {
			count++
		}
	}
	return count
}

func fetchKeyByID(ctx context.Context, gw *nexus.Gateway, keyID string) (*key.APIKey, error) {
	if gw == nil || gw.Store() == nil {
		return nil, fmt.Errorf("store unavailable")
	}
	return gw.Store().Keys().FindByID(ctx, keyID)
}

// --- Usage fetchers ---

func fetchUsageSummary(ctx context.Context, gw *nexus.Gateway, tenantID, period string) (*usage.Summary, error) {
	if gw == nil || gw.Usage() == nil {
		return nil, nil
	}
	return gw.Usage().Summary(ctx, tenantID, period)
}

func fetchMonthlySpend(ctx context.Context, gw *nexus.Gateway, tenantID string) float64 {
	if gw == nil || gw.Usage() == nil {
		return 0
	}
	spend, err := gw.Usage().MonthlySpend(ctx, tenantID)
	if err != nil {
		return 0
	}
	return spend
}

func fetchDailyRequests(ctx context.Context, gw *nexus.Gateway, tenantID string) int {
	if gw == nil || gw.Usage() == nil {
		return 0
	}
	count, err := gw.Usage().DailyRequests(ctx, tenantID)
	if err != nil {
		return 0
	}
	return count
}

func fetchUsageRecords(ctx context.Context, gw *nexus.Gateway, opts *usage.QueryOptions) ([]*usage.Record, int, error) {
	if gw == nil || gw.Usage() == nil {
		return nil, 0, nil
	}
	return gw.Usage().Query(ctx, opts)
}

func fetchRecentUsage(ctx context.Context, gw *nexus.Gateway, limit int) ([]*usage.Record, error) {
	if gw == nil || gw.Usage() == nil {
		return nil, nil
	}
	records, _, err := gw.Usage().Query(ctx, &usage.QueryOptions{Limit: limit})
	return records, err
}

// --- Model/Provider fetchers ---

func fetchModels(ctx context.Context, gw *nexus.Gateway) ([]provider.Model, error) {
	if gw == nil || gw.Models() == nil {
		return nil, nil
	}
	return gw.Models().ListModels(ctx)
}

func fetchProviderInfos(ctx context.Context, gw *nexus.Gateway) []components.ProviderInfo {
	if gw == nil || gw.Providers() == nil {
		return nil
	}
	providers := gw.Providers().All()
	healthy := gw.Providers().Healthy(ctx)
	healthySet := make(map[string]bool, len(healthy))
	for _, p := range healthy {
		healthySet[p.Name()] = true
	}

	var infos []components.ProviderInfo
	for _, p := range providers {
		models, _ := p.Models(ctx)
		infos = append(infos, components.ProviderInfo{
			Name:       p.Name(),
			ModelCount: len(models),
			Healthy:    healthySet[p.Name()],
		})
	}
	return infos
}

// --- Helpers ---

func formatTimeAgo(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = -d
	}

	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	case d < 365*24*time.Hour:
		return fmt.Sprintf("%dmo ago", int(d.Hours()/(24*30)))
	default:
		return fmt.Sprintf("%dy ago", int(d.Hours()/(24*365)))
	}
}

func formatCost(costUSD float64) string {
	if costUSD == 0 {
		return "$0.00"
	}
	if costUSD < 0.01 {
		return fmt.Sprintf("$%.4f", costUSD)
	}
	return fmt.Sprintf("$%.2f", costUSD)
}

func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func truncateString(s string, max int) string {
	if len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func formatQuotaValue(v int) string {
	if v == 0 {
		return "Unlimited"
	}
	return fmt.Sprintf("%d", v)
}

func formatBudget(v float64) string {
	if v == 0 {
		return "Unlimited"
	}
	return fmt.Sprintf("$%.2f", v)
}
