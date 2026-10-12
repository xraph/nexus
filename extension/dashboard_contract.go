package extension

import (
	dash "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"

	nexus "github.com/xraph/nexus"
	nexuscontract "github.com/xraph/nexus/extension/contract"
)

// RegisterContractContributor registers the Nexus dashboard before startup;
// its handlers resolve the gateway only after Start succeeds.
func (e *Extension) RegisterContractContributor(d *dispatcher.Dispatcher, reg dash.Registry, wreg dash.WardenRegistry) error {
	deps := nexuscontract.Deps{Gateway: func() *nexus.Gateway {
		if !e.IsStarted() {
			return nil
		}
		return e.gateway
	}}
	if logger := e.Logger(); logger != nil {
		deps.Logger = nexus.NewLogger(logger)
	}
	return nexuscontract.Register(d, reg, wreg, deps)
}
