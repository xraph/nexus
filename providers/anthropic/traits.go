package anthropic

// thinkingMode is how a model takes the thinking request field.
type thinkingMode int

const (
	// thinkingAdaptive: on is {"type":"adaptive"}, off is {"type":"disabled"}.
	thinkingAdaptive thinkingMode = iota
	// thinkingAlwaysOn: thinking cannot be turned off. On is
	// {"type":"adaptive"}, off sends no thinking field at all, because the
	// model rejects "disabled".
	thinkingAlwaysOn
	// thinkingAdaptiveBetweenTools: on is {"type":"adaptive"}, off is
	// {"type":"between_tools"}, because the model rejects "disabled".
	thinkingAdaptiveBetweenTools
	// thinkingBudget: on is {"type":"enabled","budget_tokens":N}, off sends
	// no thinking field.
	thinkingBudget
)

// traits are the per-model request rules the Messages API enforces.
type traits struct {
	thinking thinkingMode
	// fixedSampling: the model rejects temperature and top_p, so they are
	// dropped from the request.
	fixedSampling bool
	// forcedToolChoice: the model accepts tool_choice "any" and "tool".
	forcedToolChoice bool
	// efforts lists the output_config.effort levels the model accepts. Empty
	// means the model takes no effort at all.
	efforts []string
}

var (
	effortsAll       = []string{"low", "medium", "high", "xhigh", "max"}
	effortsUpToMax   = []string{"low", "medium", "high", "max"}
	effortsUpToHigh  = []string{"low", "medium", "high"}
	alwaysOnForced   = traits{thinking: thinkingAlwaysOn, fixedSampling: true, forcedToolChoice: true, efforts: effortsAll}
	alwaysOnUnforced = traits{thinking: thinkingAlwaysOn, fixedSampling: true, efforts: effortsAll}
	adaptiveFixed    = traits{thinking: thinkingAdaptive, fixedSampling: true, forcedToolChoice: true, efforts: effortsAll}
	adaptiveSampled  = traits{thinking: thinkingAdaptive, forcedToolChoice: true, efforts: effortsUpToMax}
	budgetNoEffort   = traits{thinking: thinkingBudget, forcedToolChoice: true}
	budgetWithEffort = traits{thinking: thinkingBudget, forcedToolChoice: true, efforts: effortsUpToHigh}
)

// modelTraits is keyed by catalog id; dated ids share their alias's row.
var modelTraits = map[string]traits{
	"claude-opus-5-5":   alwaysOnUnforced,
	"claude-fable-5-1":  alwaysOnUnforced,
	"claude-fable-5":    alwaysOnForced,
	"claude-sonnet-5-5": {thinking: thinkingAdaptiveBetweenTools, fixedSampling: true, efforts: effortsAll},

	"claude-opus-5":    adaptiveFixed,
	"claude-opus-4-8":  adaptiveFixed,
	"claude-opus-4-7":  adaptiveFixed,
	"claude-sonnet-5":  adaptiveFixed,
	"claude-haiku-5-5": adaptiveFixed,

	"claude-opus-4-6":   adaptiveSampled,
	"claude-sonnet-4-6": adaptiveSampled,

	"claude-opus-4-5":          budgetWithEffort,
	"claude-opus-4-5-20251101": budgetWithEffort,

	"claude-sonnet-4-5":          budgetNoEffort,
	"claude-sonnet-4-5-20250929": budgetNoEffort,
	"claude-haiku-4-5":           budgetNoEffort,
	"claude-haiku-4-5-20251001":  budgetNoEffort,
}

// traitsFor returns the model's traits and whether the model is listed. An
// unlisted id gets adaptive thinking, free sampling and forced tool choice;
// its effort is passed through unchecked and its thinking mode is picked per
// request (see thinkingFor).
func traitsFor(model string) (traits, bool) {
	t, ok := modelTraits[model]
	if !ok {
		return traits{thinking: thinkingAdaptive, forcedToolChoice: true}, false
	}
	return t, true
}
