package middlewares_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/xraph/nexus/guard"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
)

func TestRetryGivesUpOnWhatCannotSucceed(t *testing.T) {
	for name, fail := range map[string]error{
		"refusal":   &pipeline.RefusalError{Code: pipeline.CodeForbidden, Status: 403},
		"block":     &guard.BlockedError{Guard: "pii", Phase: guard.PhaseInput},
		"permanent": pipeline.Permanent(errors.New("nexus: no providers registered")),
		"canceled":  context.Canceled,
	} {
		calls := 0
		mw := middlewares.NewRetry(3, time.Millisecond, 1)
		_, err := mw.Process(context.Background(), &pipeline.Request{}, func(context.Context) (*pipeline.Response, error) {
			calls++
			return nil, fail
		})
		if calls != 1 || !errors.Is(err, fail) {
			t.Errorf("%s: calls = %d, err = %v; want one call and the same error", name, calls, err)
		}
	}
}

func TestRetryStillRetriesAProviderFailure(t *testing.T) {
	calls := 0
	mw := middlewares.NewRetry(2, time.Millisecond, 1)
	_, _ = mw.Process(context.Background(), &pipeline.Request{}, func(context.Context) (*pipeline.Response, error) {
		calls++
		return nil, errors.New("upstream 502")
	})
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}
