package middlewares_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
)

type phaseBlocker struct {
	name  string
	phase guard.Phase
}

func (b phaseBlocker) Name() string       { return b.name }
func (b phaseBlocker) Phase() guard.Phase { return b.phase }
func (b phaseBlocker) Check(context.Context, *guard.CheckInput) (*guard.CheckResult, error) {
	return &guard.CheckResult{Blocked: true, Action: guard.ActionBlock, Reason: "refused"}, nil
}

func TestGuardrailReturnsABlockedError(t *testing.T) {
	answer := &provider.CompletionResponse{
		Provider: "openai", Model: "gpt-4o",
		Choices: []provider.Choice{{Message: provider.Message{Role: "assistant", Content: "secret"}}},
		Usage:   provider.Usage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
	for _, phase := range []guard.Phase{guard.PhaseInput, guard.PhaseOutput} {
		svc := guard.NewService()
		svc.Register(phaseBlocker{"leak-check", phase})
		mw := middlewares.NewGuardrail(svc)
		req := &pipeline.Request{Completion: &provider.CompletionRequest{Messages: []provider.Message{{Role: "user", Content: "hi"}}}, State: map[string]any{}}
		_, err := mw.Process(context.Background(), req, func(context.Context) (*pipeline.Response, error) {
			return &pipeline.Response{Completion: answer}, nil
		})
		var blocked *guard.BlockedError
		if !errors.As(err, &blocked) || blocked.Guard != "leak-check" || blocked.Phase != phase {
			t.Fatalf("%s: err = %v", phase, err)
		}
		if phase == guard.PhaseOutput && (blocked.Usage == nil || blocked.Usage.TotalTokens != 15 || blocked.Model != "gpt-4o" || blocked.Provider != "openai") {
			t.Fatalf("output block must carry what the refused response consumed: %+v", blocked)
		}
		if phase == guard.PhaseInput && blocked.Usage != nil {
			t.Fatalf("an input block has no usage: %+v", blocked)
		}
	}
}
