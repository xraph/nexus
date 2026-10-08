package contract

import (
	"context"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/tenant"
	"github.com/xraph/nexus/usage"
)

type tenantCounts struct {
	Total     int `json:"total"`
	Active    int `json:"active"`
	Disabled  int `json:"disabled"`
	Suspended int `json:"suspended"`
}
type postureView struct {
	RequireAPIKey       bool   `json:"requireApiKey"`
	AuthenticationScope string `json:"authenticationScope"`
	LimiterKind         string `json:"limiterKind"`
	UsageEnabled        bool   `json:"usageEnabled"`
	GuardCount          int    `json:"guardCount"`
	CacheKind           string `json:"cacheKind"`
}
type overviewResponse struct {
	Tenants          tenantCounts          `json:"tenants"`
	ActiveKeys       int                   `json:"activeKeys"`
	MonthSpendUSD    *string               `json:"monthSpendUsd"`
	UnpricedRequests *int                  `json:"unpricedRequests"`
	RequestsToday    *int                  `json:"requestsToday"`
	ByOutcome        map[usage.Outcome]int `json:"byOutcome"`
	OutcomePeriod    string                `json:"outcomePeriod"`
	Posture          postureView           `json:"posture"`
	InsertErrors     int64                 `json:"insertErrors"`
	LimiterErrors    int64                 `json:"limiterErrors"`
}

func gatewayPosture(gw *nexus.Gateway) postureView {
	p := postureView{RequireAPIKey: gw.Config().RequireAPIKey, AuthenticationScope: "HTTP api and proxy routes", UsageEnabled: gw.Config().EnableUsage, LimiterKind: "unknown", CacheKind: "none"}
	if gw.Limiter() != nil {
		p.LimiterKind = gw.Limiter().Kind()
	}
	if gw.Guard() != nil {
		p.GuardCount = len(gw.Guard().List())
	}
	completion, stream := gw.CacheKinds()
	p.CacheKind = completion
	if completion == "none" {
		p.CacheKind = stream
	} else if stream != "none" && stream != completion {
		p.CacheKind = "mixed"
	}

	return p
}
func overviewGet(ctx context.Context, gw *nexus.Gateway, _ struct{}) (overviewResponse, error) {
	out := overviewResponse{Posture: gatewayPosture(gw), OutcomePeriod: "month", InsertErrors: gw.UsageInsertErrors(), LimiterErrors: gw.LimiterErrors()}
	cursor := ""
	for {
		page, err := gw.Tenants().List(ctx, &tenant.ListOptions{Limit: paging.MaxLimit, Cursor: cursor})
		if err != nil {
			return overviewResponse{}, err
		}
		for _, t := range page.Items {
			out.Tenants.Total++
			switch t.Status {
			case tenant.StatusActive:
				out.Tenants.Active++
			case tenant.StatusDisabled:
				out.Tenants.Disabled++
			case tenant.StatusSuspended:
				out.Tenants.Suspended++
			}
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
	}
	count, err := gw.Keys().Count(ctx, &key.ListOptions{Status: key.KeyActive})
	if err != nil {
		return overviewResponse{}, err
	}
	out.ActiveKeys = count
	if !gw.Config().EnableUsage {
		return out, nil
	}
	summary, err := gw.Usage().Summary(ctx, "", "month")
	if err != nil {
		return overviewResponse{}, err
	}
	today, err := gw.Usage().DailyRequests(ctx, "")
	if err != nil {
		return overviewResponse{}, err
	}
	out.MonthSpendUSD = ptr(summary.TotalCostUSD.String())
	out.UnpricedRequests = ptr(summary.UnpricedRequests)
	out.RequestsToday = ptr(today)
	out.ByOutcome = outcomeCounts(summary.ByOutcome)
	return out, nil
}
