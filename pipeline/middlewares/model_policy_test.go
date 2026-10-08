package middlewares_test

import (
	"context"
	"strings"
	"testing"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/tenant"
)

// policyRun runs the model policy stage for a tenant with cfg. requested is
// the name the caller asked for and resolved the model after alias
// resolution; they differ when an alias stage rewrote the request. It
// reports whether the request reached next.
func policyRun(t *testing.T, cfg *tenant.Config, requested, resolved string, embedding bool) (reached bool, err error) {
	t.Helper()
	ctx := context.Background()
	if cfg != nil {
		ctx = middlewares.WithTenantForTest(ctx, &tenant.Tenant{Config: *cfg})
	}
	req := &pipeline.Request{State: map[string]any{}}
	if embedding {
		req.Type = pipeline.RequestEmbedding
		req.Embedding = &provider.EmbeddingRequest{Model: resolved}
	} else {
		req.Type = pipeline.RequestCompletion
		req.Completion = &provider.CompletionRequest{Model: resolved}
		if requested != resolved {
			req.State[pipeline.StateOriginalModel] = requested
		}
	}
	_, err = middlewares.NewModelPolicy().Process(ctx, req, func(context.Context) (*pipeline.Response, error) {
		reached = true
		return &pipeline.Response{}, nil
	})
	return reached, err
}

func wantForbidden(t *testing.T, err error, reached bool, model string) {
	t.Helper()
	status, code := pipeline.HTTPStatus(err)
	if err == nil || status != 403 || code != pipeline.CodeForbidden {
		t.Fatalf("err = %v (status %d, code %q); want 403 forbidden", err, status, code)
	}
	if reached {
		t.Fatal("a refused request must not reach next")
	}
	if !strings.Contains(err.Error(), model) {
		t.Fatalf("message %q does not name the model %q", err.Error(), model)
	}
}

func TestModelPolicyIsNamedAndOrdered(t *testing.T) {
	mw := middlewares.NewModelPolicy()
	if mw.Name() != "model_policy" || mw.Priority() != 260 {
		t.Fatalf("name %q, priority %d; want model_policy at 260", mw.Name(), mw.Priority())
	}
}

func TestModelPolicyRefusesABlockedRequestedName(t *testing.T) {
	reached, err := policyRun(t, &tenant.Config{BlockedModels: []string{"smart"}}, "smart", "o1", false)
	wantForbidden(t, err, reached, "smart")
}

func TestModelPolicyRefusesABlockedResolvedName(t *testing.T) {
	reached, err := policyRun(t, &tenant.Config{BlockedModels: []string{"o1"}}, "smart", "o1", false)
	wantForbidden(t, err, reached, "o1")
}

func TestModelPolicyAllowsAnAliasNamedInTheAllowList(t *testing.T) {
	reached, err := policyRun(t, &tenant.Config{AllowedModels: []string{"smart"}}, "smart", "o1", false)
	if err != nil || !reached {
		t.Fatalf("reached %v, err %v; want the alias admitted", reached, err)
	}
}

func TestModelPolicyAllowsAResolvedNameInTheAllowList(t *testing.T) {
	reached, err := policyRun(t, &tenant.Config{AllowedModels: []string{"o1"}}, "smart", "o1", false)
	if err != nil || !reached {
		t.Fatalf("reached %v, err %v; want the resolved model admitted", reached, err)
	}
}

func TestModelPolicyRefusesANameTheAllowListLacks(t *testing.T) {
	reached, err := policyRun(t, &tenant.Config{AllowedModels: []string{"gpt-4o-mini"}}, "smart", "o1", false)
	wantForbidden(t, err, reached, "smart")
}

func TestModelPolicyBlocksWinOverAllows(t *testing.T) {
	// Inserted without the service, so its validation does not stop the overlap.
	cfg := &tenant.Config{AllowedModels: []string{"gpt-4o"}, BlockedModels: []string{"gpt-4o"}}
	reached, err := policyRun(t, cfg, "gpt-4o", "gpt-4o", false)
	wantForbidden(t, err, reached, "gpt-4o")
}

func TestModelPolicyChecksEmbeddingsToo(t *testing.T) {
	reached, err := policyRun(t, &tenant.Config{BlockedModels: []string{"text-embedding-3-small"}}, "", "text-embedding-3-small", true)
	wantForbidden(t, err, reached, "text-embedding-3-small")

	reached, err = policyRun(t, &tenant.Config{AllowedModels: []string{"text-embedding-3-small"}}, "", "text-embedding-3-small", true)
	if err != nil || !reached {
		t.Fatalf("an allowed embedding model: reached %v, err %v", reached, err)
	}
}

func TestModelPolicyPassesWhenThereIsNoTenant(t *testing.T) {
	reached, err := policyRun(t, nil, "gpt-4o", "gpt-4o", false)
	if err != nil || !reached {
		t.Fatalf("reached %v, err %v; want a request with no tenant to pass", reached, err)
	}
}

func TestModelPolicyPassesWhenTheListsAreEmpty(t *testing.T) {
	reached, err := policyRun(t, &tenant.Config{}, "gpt-4o", "gpt-4o", false)
	if err != nil || !reached {
		t.Fatalf("reached %v, err %v; want empty lists to pass", reached, err)
	}
}
