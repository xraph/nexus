package guard

import "github.com/xraph/nexus/provider"

// BlockedError reports that a guard refused a request. Guard names the guard
// that blocked it. An output-phase block happens after the provider answered
// and charged for it, so Usage, Model and Provider carry what the refused
// response consumed; they are empty for an input block.
type BlockedError struct {
	Guard    string
	Phase    Phase
	Reason   string
	Usage    *provider.Usage
	Model    string
	Provider string
}

func (e *BlockedError) Error() string {
	return "nexus: blocked by guard " + e.Guard + ": " + e.Reason
}

// RefusalCode and StatusCode let an HTTP edge map a block to 400 the way it
// maps any other refusal. The usage stage still records it as blocked.
func (e *BlockedError) RefusalCode() string { return "content_blocked" }
func (e *BlockedError) StatusCode() int     { return 400 }
