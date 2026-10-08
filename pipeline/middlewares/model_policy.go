package middlewares

import (
	"context"
	"fmt"
	"slices"

	"github.com/xraph/nexus/pipeline"
)

// ModelPolicyMiddleware refuses a model the tenant blocks, or does not
// allow. It runs after alias resolution, so it sees both the name the
// caller asked for and the model it resolved to. Either name being blocked
// refuses the request, and either being allowed admits it. Blocks win.
// Names match exactly. A request with no tenant, or a tenant with neither
// list, passes.
type ModelPolicyMiddleware struct{}

// NewModelPolicy returns the model policy stage.
func NewModelPolicy() *ModelPolicyMiddleware { return &ModelPolicyMiddleware{} }

func (*ModelPolicyMiddleware) Name() string  { return "model_policy" }
func (*ModelPolicyMiddleware) Priority() int { return 260 }

func (*ModelPolicyMiddleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	t, ok := TenantFromContext(ctx)
	if !ok || (len(t.Config.AllowedModels) == 0 && len(t.Config.BlockedModels) == 0) {
		return next(ctx)
	}
	// Alias resolution only runs for completions, so for an embedding the
	// requested name and the resolved name are the same.
	resolved := requestModel(req)
	requested := resolved
	if s, ok := req.State[pipeline.StateOriginalModel].(string); ok && s != "" {
		requested = s
	}
	for _, n := range []string{requested, resolved} {
		if slices.Contains(t.Config.BlockedModels, n) {
			return nil, refuse(pipeline.CodeForbidden, 403, describeModel(n)+" is blocked for this tenant")
		}
	}
	if len(t.Config.AllowedModels) > 0 &&
		!slices.Contains(t.Config.AllowedModels, requested) && !slices.Contains(t.Config.AllowedModels, resolved) {
		return nil, refuse(pipeline.CodeForbidden, 403, describeModel(requested)+" is not allowed for this tenant")
	}
	return next(ctx)
}

// describeModel names a model for a refusal message, quoted so that a name
// with odd characters cannot run into the sentence.
func describeModel(name string) string {
	if name == "" {
		return "a request with no model named"
	}
	return fmt.Sprintf("model %q", name)
}
