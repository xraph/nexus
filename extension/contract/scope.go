package contract

import (
	"context"
	"encoding/json"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/id"
)

// tenantScope distinguishes an omitted operator-wide filter from an invalid
// explicit filter. Principal claims never supply a tenant for this dashboard.
func tenantScope(ctx context.Context, gw *nexus.Gateway, raw json.RawMessage) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return "", badRequest("tenantId must be a tenant ID string")
	}
	if _, err := id.ParseTenantID(value); err != nil {
		return "", badRequest("tenantId must be a tenant ID string")
	}
	if _, err := gw.Tenants().Get(ctx, value); err != nil {
		return "", mapError(err)
	}
	return value, nil
}
