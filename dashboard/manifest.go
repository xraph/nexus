package dashboard

import (
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// NewManifest builds a contributor.Manifest for the nexus dashboard.
func NewManifest() *contributor.Manifest {
	return &contributor.Manifest{
		Name:        "nexus",
		DisplayName: "Nexus",
		Icon:        "cpu",
		Version:     "1.0.0",
		Layout:      "extension",
		ShowSidebar: boolPtr(true),
		TopbarConfig: &contributor.TopbarConfig{
			Title:       "Nexus",
			LogoIcon:    "cpu",
			AccentColor: "#10b981",
			ShowSearch:  true,
			Actions: []contributor.TopbarAction{
				{Label: "API Docs", Icon: "file-text", Href: "/docs", Variant: "ghost"},
			},
		},
		Nav:      baseNav(),
		Widgets:  baseWidgets(),
		Settings: baseSettings(),
		Capabilities: []string{
			"searchable",
		},
	}
}

// baseNav returns the core navigation items for the nexus dashboard.
func baseNav() []contributor.NavItem {
	return []contributor.NavItem{
		// Overview
		{Label: "Overview", Path: "/", Icon: "layout-dashboard", Group: "Overview", Priority: 0},

		// Gateway
		{Label: "Tenants", Path: "/tenants", Icon: "users", Group: "Gateway", Priority: 0},
		{Label: "API Keys", Path: "/keys", Icon: "key-round", Group: "Gateway", Priority: 1},

		// Analytics
		{Label: "Usage", Path: "/usage", Icon: "bar-chart-3", Group: "Analytics", Priority: 0},
		{Label: "Usage Records", Path: "/usage/records", Icon: "list", Group: "Analytics", Priority: 1},

		// System
		{Label: "Models & Providers", Path: "/models", Icon: "cpu", Group: "System", Priority: 0},
		{Label: "Settings", Path: "/settings", Icon: "settings", Group: "System", Priority: 1},
	}
}

// baseWidgets returns the core widget descriptors for the nexus dashboard.
func baseWidgets() []contributor.WidgetDescriptor {
	return []contributor.WidgetDescriptor{
		{
			ID:          "nexus-stats",
			Title:       "Gateway Stats",
			Description: "Key gateway metrics",
			Size:        "md",
			RefreshSec:  30,
			Group:       "Nexus",
		},
		{
			ID:          "nexus-monthly-spend",
			Title:       "Monthly Spend",
			Description: "Current month LLM spend",
			Size:        "sm",
			RefreshSec:  60,
			Group:       "Nexus",
		},
		{
			ID:          "nexus-recent-activity",
			Title:       "Recent Activity",
			Description: "Latest API requests",
			Size:        "lg",
			RefreshSec:  15,
			Group:       "Nexus",
		},
	}
}

// baseSettings returns the core settings descriptors for the nexus dashboard.
func baseSettings() []contributor.SettingsDescriptor {
	return []contributor.SettingsDescriptor{
		{
			ID:          "nexus-config",
			Title:       "Gateway Configuration",
			Description: "Nexus AI gateway settings",
			Group:       "Nexus",
			Icon:        "cpu",
		},
	}
}

func boolPtr(b bool) *bool { return &b }
