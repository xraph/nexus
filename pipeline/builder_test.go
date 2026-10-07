package pipeline_test

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

type stage struct {
	name     string
	priority int
	log      *[]string
}

func (s stage) Name() string  { return s.name }
func (s stage) Priority() int { return s.priority }
func (s stage) Process(ctx context.Context, _ *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	*s.log = append(*s.log, s.name)
	return next(ctx)
}

type terminal struct{ stage }

func (terminal) Terminal() {}
func (t terminal) Process(_ context.Context, _ *pipeline.Request, _ pipeline.NextFunc) (*pipeline.Response, error) {
	*t.log = append(*t.log, t.name)
	return &pipeline.Response{Completion: &provider.CompletionResponse{}}, nil
}

func TestTheTerminalRunsLastWhateverItsPriority(t *testing.T) {
	var log []string
	svc, err := pipeline.NewBuilder().Use(
		terminal{stage{"call", 350, &log}},
		stage{"usage", 550, &log},
		stage{"custom", 400, &log},
		stage{"timeout", 20, &log},
		stage{"also-20", 20, &log},
	).Build()
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := svc.Execute(context.Background(), &provider.CompletionRequest{}); err != nil {
		t.Fatalf("execute: %v", err)
	}
	want := []string{"timeout", "also-20", "custom", "usage", "call"}
	if !slices.Equal(log, want) {
		t.Fatalf("ran %v, want %v", log, want)
	}
	stages := svc.(pipeline.Inspector).Stages()
	if len(stages) != 5 || stages[4] != (pipeline.Stage{Name: "call", Priority: 350, Terminal: true}) || stages[0].Terminal {
		t.Fatalf("stages = %+v", stages)
	}
}

func TestAPipelineNeedsExactlyOneTerminal(t *testing.T) {
	var log []string
	if _, err := pipeline.NewBuilder().Use(stage{"a", 1, &log}).Build(); !errors.Is(err, pipeline.ErrNoTerminal) {
		t.Fatalf("no terminal = %v", err)
	}
	two := pipeline.NewBuilder().Use(terminal{stage{"x", 1, &log}}, terminal{stage{"y", 2, &log}})
	if _, err := two.Build(); !errors.Is(err, pipeline.ErrManyTerminals) {
		t.Fatalf("two terminals = %v", err)
	}
}
