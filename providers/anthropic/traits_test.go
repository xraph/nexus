package anthropic

import (
	"slices"
	"testing"
)

func TestTraitsTable(t *testing.T) {
	newEfforts := []string{"low", "medium", "high", "xhigh", "max"}
	tests := []struct {
		model            string
		thinking         thinkingMode
		fixedSampling    bool
		forcedToolChoice bool
		efforts          []string
	}{
		{"claude-opus-5-5", thinkingAlwaysOn, true, false, newEfforts},
		{"claude-fable-5-1", thinkingAlwaysOn, true, false, newEfforts},
		{"claude-fable-5", thinkingAlwaysOn, true, true, newEfforts},
		{"claude-sonnet-5-5", thinkingAdaptiveBetweenTools, true, false, newEfforts},
		{"claude-opus-5", thinkingAdaptive, true, true, newEfforts},
		{"claude-opus-4-8", thinkingAdaptive, true, true, newEfforts},
		{"claude-opus-4-7", thinkingAdaptive, true, true, newEfforts},
		{"claude-sonnet-5", thinkingAdaptive, true, true, newEfforts},
		{"claude-haiku-5-5", thinkingAdaptive, true, true, newEfforts},
		{"claude-opus-4-6", thinkingAdaptive, false, true, []string{"low", "medium", "high", "max"}},
		{"claude-sonnet-4-6", thinkingAdaptive, false, true, []string{"low", "medium", "high", "max"}},
		{"claude-opus-4-5", thinkingBudget, false, true, []string{"low", "medium", "high"}},
		{"claude-opus-4-5-20251101", thinkingBudget, false, true, []string{"low", "medium", "high"}},
		{"claude-sonnet-4-5", thinkingBudget, false, true, nil},
		{"claude-sonnet-4-5-20250929", thinkingBudget, false, true, nil},
		{"claude-haiku-4-5", thinkingBudget, false, true, nil},
		{"claude-haiku-4-5-20251001", thinkingBudget, false, true, nil},
	}
	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			tr, ok := traitsFor(tt.model)
			if !ok {
				t.Fatalf("traitsFor(%q) not listed", tt.model)
			}
			if tr.thinking != tt.thinking {
				t.Errorf("thinking = %v, want %v", tr.thinking, tt.thinking)
			}
			if tr.fixedSampling != tt.fixedSampling {
				t.Errorf("fixedSampling = %v, want %v", tr.fixedSampling, tt.fixedSampling)
			}
			if tr.forcedToolChoice != tt.forcedToolChoice {
				t.Errorf("forcedToolChoice = %v, want %v", tr.forcedToolChoice, tt.forcedToolChoice)
			}
			if !slices.Equal(tr.efforts, tt.efforts) {
				t.Errorf("efforts = %v, want %v", tr.efforts, tt.efforts)
			}
		})
	}
}

func TestTraitsForUnlistedModel(t *testing.T) {
	for _, id := range []string{"foo", "claude-sonnet-4-5-20250514", "claude-3-5-haiku-20241022"} {
		tr, ok := traitsFor(id)
		if ok {
			t.Errorf("traitsFor(%q) listed, want unlisted", id)
		}
		if tr.thinking != thinkingAdaptive || tr.fixedSampling || !tr.forcedToolChoice || tr.efforts != nil {
			t.Errorf("traitsFor(%q) = %+v, want the unlisted defaults", id, tr)
		}
	}
}

// Every catalog id must have a traits row, and the reverse, so a model added
// to one table is not silently shaped by the unlisted defaults.
func TestTraitsCoverTheCatalog(t *testing.T) {
	for _, m := range anthropicModels() {
		if _, ok := traitsFor(m.ID); !ok {
			t.Errorf("catalog model %q has no traits row", m.ID)
		}
	}
	for id := range modelTraits {
		if _, ok := catalogModel(id); !ok {
			t.Errorf("traits row %q is not in the catalog", id)
		}
	}
}
