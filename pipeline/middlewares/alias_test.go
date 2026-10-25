package middlewares_test

import (
	"context"
	"testing"

	"github.com/xraph/nexus/model"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
)

func aliasStage(t *testing.T, targets ...model.AliasTarget) *middlewares.AliasMiddleware {
	t.Helper()
	aliases := model.NewAliasRegistry()
	if err := aliases.Register(&model.Alias{Name: "smart", Targets: targets}); err != nil {
		t.Fatal(err)
	}
	return middlewares.NewAlias(aliases)
}

func passThrough(context.Context) (*pipeline.Response, error) { return &pipeline.Response{}, nil }

func TestAliasFiltersTargetsByExplicitProvider(t *testing.T) {
	stage := aliasStage(t,
		model.AliasTarget{Provider: "openai", Model: "gpt-4o"},
		model.AliasTarget{Provider: "anthropic", Model: "claude-sonnet-5-5"},
	)
	for range 50 {
		req := routedCompletion("smart", "anthropic")
		if _, err := stage.Process(context.Background(), req, passThrough); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if req.Completion.Model != "claude-sonnet-5-5" {
			t.Fatalf("model = %q, an explicit anthropic request must get anthropic's target", req.Completion.Model)
		}
		if req.State[pipeline.StateAliasProvider] != "anthropic" || req.State[pipeline.StateAliasModel] != "claude-sonnet-5-5" {
			t.Fatalf("alias state = %v, %v", req.State[pipeline.StateAliasProvider], req.State[pipeline.StateAliasModel])
		}
		if req.State[pipeline.StateOriginalModel] != "smart" {
			t.Fatalf("original model = %v, want smart", req.State[pipeline.StateOriginalModel])
		}
	}
}

func TestAliasWithNoTargetForExplicitProviderIsLeftAlone(t *testing.T) {
	stage := aliasStage(t, model.AliasTarget{Provider: "openai", Model: "gpt-4o"})

	req := routedCompletion("smart", "anthropic")
	if _, err := stage.Process(context.Background(), req, passThrough); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if req.Completion.Model != "smart" {
		t.Fatalf("model = %q, want it left as smart", req.Completion.Model)
	}
	for _, key := range []string{pipeline.StateAliasProvider, pipeline.StateAliasModel, pipeline.StateOriginalModel} {
		if _, ok := req.State[key]; ok {
			t.Fatalf("state has %s, the alias must not be applied", key)
		}
	}
}

func TestAliasTargetWithoutProviderMatchesAnExplicitProvider(t *testing.T) {
	stage := aliasStage(t,
		model.AliasTarget{Provider: "openai", Model: "gpt-4o"},
		model.AliasTarget{Model: "claude-sonnet-5-5"},
	)
	for range 20 {
		req := routedCompletion("smart", "anthropic")
		if _, err := stage.Process(context.Background(), req, passThrough); err != nil {
			t.Fatalf("Process: %v", err)
		}
		if req.Completion.Model != "claude-sonnet-5-5" {
			t.Fatalf("model = %q, a target with no provider fits any explicit provider", req.Completion.Model)
		}
	}
}
