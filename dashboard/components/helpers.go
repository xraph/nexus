package components

import (
	"fmt"
	"time"

	"github.com/xraph/nexus/money"
)

// formatTimeAgo returns a human-readable relative time string.
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

// formatQuotaValue formats a quota integer.
func formatQuotaValue(v int) string {
	if v == 0 {
		return "Unlimited"
	}
	return fmt.Sprintf("%d", v)
}

// formatBudget formats a monthly budget; zero means no budget.
func formatBudget(v money.USD) string {
	if v.IsZero() {
		return "Unlimited"
	}
	return "$" + v.String()
}

// formatCost formats an exact USD amount for the legacy dashboard.
func formatCost(cost money.USD) string { return "$" + cost.String() }

// formatCostPtr formats a cost that may be unknown.
func formatCostPtr(cost *money.USD) string {
	if cost == nil {
		return "unpriced"
	}
	return formatCost(*cost)
}

// formatDuration formats a duration for display.
func formatDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}
