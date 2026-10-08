package contract

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/paging"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/usage"
)

type capabilitiesView struct {
	Chat               bool `json:"chat"`
	Streaming          bool `json:"streaming"`
	Embeddings         bool `json:"embeddings"`
	Images             bool `json:"images"`
	Vision             bool `json:"vision"`
	Tools              bool `json:"tools"`
	JSON               bool `json:"json"`
	Audio              bool `json:"audio"`
	Thinking           bool `json:"thinking"`
	Batch              bool `json:"batch"`
	StreamingReasoning bool `json:"streamingReasoning"`
	StreamingTools     bool `json:"streamingTools"`
	StreamingAudio     bool `json:"streamingAudio"`
	StreamingCitations bool `json:"streamingCitations"`
	RealtimeAudio      bool `json:"realtimeAudio"`
	RealtimeVideo      bool `json:"realtimeVideo"`
	LiveBidi           bool `json:"liveBidi"`
}

func projectCapabilities(c provider.Capabilities) capabilitiesView {
	return capabilitiesView{Chat: c.Chat, Streaming: c.Streaming, Embeddings: c.Embeddings, Images: c.Images, Vision: c.Vision, Tools: c.Tools, JSON: c.JSON, Audio: c.Audio, Thinking: c.Thinking, Batch: c.Batch, StreamingReasoning: c.StreamingReasoning, StreamingTools: c.StreamingTools, StreamingAudio: c.StreamingAudio, StreamingCitations: c.StreamingCitations, RealtimeAudio: c.RealtimeAudio, RealtimeVideo: c.RealtimeVideo, LiveBidi: c.LiveBidi}
}

type modelRow struct {
	ID                     string           `json:"id"`
	Provider               string           `json:"provider"`
	Name                   string           `json:"name"`
	Capabilities           capabilitiesView `json:"capabilities"`
	ContextWindow          int              `json:"contextWindow"`
	MaxOutput              int              `json:"maxOutput"`
	Priced                 bool             `json:"priced"`
	Free                   bool             `json:"free"`
	InputPerMillionUSD     *string          `json:"inputPerMillionUsd"`
	OutputPerMillionUSD    *string          `json:"outputPerMillionUsd"`
	EmbeddingPerMillionUSD *string          `json:"embeddingPerMillionUsd"`
}
type modelsResponse struct {
	Items []modelRow `json:"items"`
}

func modelsList(ctx context.Context, gw *nexus.Gateway, _ struct{}) (modelsResponse, error) {
	out := modelsResponse{Items: []modelRow{}}
	// The default model service skips provider errors. A dashboard catalog
	// must surface a failed read rather than present an incomplete list as all.
	for _, p := range gw.Providers().All() {
		models, err := p.Models(ctx)
		if err != nil {
			return modelsResponse{}, fmt.Errorf("provider %s model catalog: %w", p.Name(), err)
		}
		for _, m := range models {
			price := m.Pricing
			if f, ok := p.(provider.FreeOfCharge); ok && f.FreeOfCharge() {
				price = provider.Pricing{Free: true}
			}
			if price.Free {
				price = provider.Pricing{Free: true}
			}
			row := modelRow{ID: m.ID, Provider: p.Name(), Name: m.Name, Capabilities: projectCapabilities(m.Capabilities), ContextWindow: m.ContextWindow, MaxOutput: m.MaxOutput, Free: price.Free}
			if price.Free || !price.InputPerMillion.IsZero() || !price.OutputPerMillion.IsZero() {
				row.InputPerMillionUSD = ptr(price.InputPerMillion.String())
				row.OutputPerMillionUSD = ptr(price.OutputPerMillion.String())
				row.Priced = true
			}
			if price.Free || !price.EmbeddingPerMillion.IsZero() {
				row.EmbeddingPerMillionUSD = ptr(price.EmbeddingPerMillion.String())
				row.Priced = true
			}
			out.Items = append(out.Items, row)
		}
	}
	slices.SortFunc(out.Items, func(a, b modelRow) int {
		if n := strings.Compare(a.Provider, b.Provider); n != 0 {
			return n
		}
		return strings.Compare(a.ID, b.ID)
	})
	return out, nil
}

type providerRow struct {
	Name         string           `json:"name"`
	Capabilities capabilitiesView `json:"capabilities"`
	ModelCount   int              `json:"modelCount"`
	Requests     *int             `json:"requests"`
	Errors       *int             `json:"errors"`
}
type providersResponse struct {
	Items        []providerRow `json:"items"`
	UsageEnabled bool          `json:"usageEnabled"`
	From         *string       `json:"from"`
	To           *string       `json:"to"`
}

func providersList(ctx context.Context, gw *nexus.Gateway, _ struct{}) (providersResponse, error) {
	now := time.Now().UTC()
	start := now.Add(-15 * time.Minute)
	out := providersResponse{Items: []providerRow{}, UsageEnabled: gw.Config().EnableUsage, From: formatTime(start), To: formatTime(now)}
	counts := map[string]int{}
	errors := map[string]int{}
	if out.UsageEnabled {
		opts := &usage.QueryOptions{StartTime: start, EndTime: now, Limit: paging.MaxLimit}
		for {
			page, err := gw.Usage().Query(ctx, opts)
			if err != nil {
				return providersResponse{}, err
			}
			for _, r := range page.Items {
				counts[r.Provider]++
				if r.Outcome == usage.OutcomeError {
					errors[r.Provider]++
				}
			}
			if page.NextCursor == "" {
				break
			}
			opts.Cursor = page.NextCursor
		}
	}
	for _, p := range gw.Providers().All() {
		models, err := p.Models(ctx)
		if err != nil {
			return providersResponse{}, fmt.Errorf("provider %s model catalog: %w", p.Name(), err)
		}
		row := providerRow{Name: p.Name(), Capabilities: projectCapabilities(p.Capabilities()), ModelCount: len(models)}
		if out.UsageEnabled {
			row.Requests = ptr(counts[p.Name()])
			row.Errors = ptr(errors[p.Name()])
		}
		out.Items = append(out.Items, row)
	}
	slices.SortFunc(out.Items, func(a, b providerRow) int { return strings.Compare(a.Name, b.Name) })
	return out, nil
}

type stageRow struct {
	Name     string `json:"name"`
	Priority int    `json:"priority"`
	Terminal bool   `json:"terminal"`
}
type guardRow struct {
	Name  string `json:"name"`
	Phase string `json:"phase"`
}
type transformRow struct {
	Name      string `json:"name"`
	Phase     string `json:"phase"`
	Streaming bool   `json:"streaming"`
}
type aliasRow struct {
	Name            string                         `json:"name"`
	Targets         []model.AliasTarget            `json:"targets"`
	TenantOverrides map[string][]model.AliasTarget `json:"tenantOverrides"`
}
type cacheView struct {
	Kind       string   `json:"kind"`
	StreamKind string   `json:"streamKind"`
	Hits       *int64   `json:"hits"`
	Misses     *int64   `json:"misses"`
	HitRate    *float64 `json:"hitRate"`
	Size       *int64   `json:"size"`
	Bytes      *int64   `json:"bytes"`
	StatsScope string   `json:"statsScope"`
}
type gatewayResponse struct {
	Stages             []stageRow     `json:"stages"`
	StagesAvailable    bool           `json:"stagesAvailable"`
	RoutingStrategy    string         `json:"routingStrategy"`
	RoutingCaveats     []string       `json:"routingCaveats"`
	Guards             []guardRow     `json:"guards"`
	GuardCaveats       []string       `json:"guardCaveats"`
	Cache              cacheView      `json:"cache"`
	Aliases            []aliasRow     `json:"aliases"`
	Transforms         []transformRow `json:"transforms"`
	Posture            postureView    `json:"posture"`
	EnforcementCaveats []string       `json:"enforcementCaveats"`
}

func gatewayGet(ctx context.Context, gw *nexus.Gateway, _ struct{}) (gatewayResponse, error) {
	out := gatewayResponse{Stages: []stageRow{}, RoutingStrategy: gw.RoutingStrategy(), RoutingCaveats: []string{}, Guards: []guardRow{}, GuardCaveats: []string{"The default pipeline does not guard streamed output. Built-in text guards skip non-text content."}, Aliases: []aliasRow{}, Transforms: []transformRow{}, Posture: gatewayPosture(gw), EnforcementCaveats: []string{"Monthly budgets are soft limits. In-flight requests, unpriced calls and records that fail to store can exceed the budget.", "Limiter failures allow requests through; limiterErrors counts them."}}
	_, out.StagesAvailable = gw.Pipeline().(pipeline.Inspector)
	for _, s := range gw.PipelineStages() {
		out.Stages = append(out.Stages, stageRow{Name: s.Name, Priority: s.Priority, Terminal: s.Terminal})
	}
	if out.RoutingStrategy == "cost_optimized" || out.RoutingStrategy == "latency_optimized" {
		out.RoutingCaveats = append(out.RoutingCaveats, "Picks the first healthy provider while candidates have no cost or latency data.")
	}
	if gw.Guard() != nil {
		for _, g := range gw.Guard().List() {
			out.Guards = append(out.Guards, guardRow{Name: g.Name(), Phase: string(g.Phase())})
		}
	}
	out.Cache.Kind, out.Cache.StreamKind = gw.CacheKinds()
	out.Cache.StatsScope = "completion cache since startup"
	if c := gw.Cache(); c != nil {
		stats, err := c.Stats(ctx)
		if err != nil {
			return gatewayResponse{}, err
		}
		if stats != nil {
			out.Cache.Hits = ptr(stats.Hits)
			out.Cache.Misses = ptr(stats.Misses)
			out.Cache.HitRate = ptr(stats.HitRate)
		}
	}
	for _, a := range gw.Aliases() {
		overrides := make(map[string][]model.AliasTarget, len(a.TenantOverrides))
		for tid, targets := range a.TenantOverrides {
			overrides[tid] = nonNil(targets)
		}
		out.Aliases = append(out.Aliases, aliasRow{Name: a.Name, Targets: nonNil(a.Targets), TenantOverrides: overrides})
	}
	slices.SortFunc(out.Aliases, func(a, b aliasRow) int { return strings.Compare(a.Name, b.Name) })
	if r := gw.Transforms(); r != nil {
		streaming := map[string]bool{}
		for _, t := range r.StreamingOutputs() {
			streaming[t.Name()] = true
		}
		for _, t := range r.InputTransforms() {
			out.Transforms = append(out.Transforms, transformRow{Name: t.Name(), Phase: "input"})
		}
		for _, t := range r.OutputTransforms() {
			out.Transforms = append(out.Transforms, transformRow{Name: t.Name(), Phase: "output", Streaming: streaming[t.Name()]})
		}
	}
	if !out.Posture.UsageEnabled {
		out.EnforcementCaveats = append(out.EnforcementCaveats, "Usage collection is off, so monthly budgets cannot apply. Daily, RPM, TPM and token caps still apply.")
	}
	if out.Posture.LimiterKind == "memory" {
		out.EnforcementCaveats = append(out.EnforcementCaveats, "Memory limits apply per replica and reset on restart.")
	}
	return out, nil
}
