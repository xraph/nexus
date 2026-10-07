package middlewares_test

import (
	"context"
	"errors"
	"fmt"
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

func TestRetryStopsWhenTheRequestContextIsDone(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	mw := middlewares.NewRetry(3, time.Millisecond, 1)
	_, err := mw.Process(ctx, &pipeline.Request{}, func(context.Context) (*pipeline.Response, error) {
		calls++
		cancel()
		return nil, context.Canceled
	})
	if calls != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("calls = %d, err = %v; want one call and context.Canceled", calls, err)
	}
}

func TestRetryStillRetriesAProviderClientTimeout(t *testing.T) {
	calls := 0
	mw := middlewares.NewRetry(2, time.Millisecond, 1)
	_, err := mw.Process(context.Background(), &pipeline.Request{}, func(context.Context) (*pipeline.Response, error) {
		calls++
		return nil, fmt.Errorf("provider: %w", context.DeadlineExceeded)
	})
	if calls != 3 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("calls = %d, err = %v; want 3 calls and the timeout", calls, err)
	}
}

func TestAGuardBlockMapsToContentBlockedAt400(t *testing.T) {
	status, code := pipeline.HTTPStatus(&guard.BlockedError{Guard: "pii"})
	if status != 400 || code != pipeline.CodeContentBlocked {
		t.Fatalf("HTTPStatus = %d %q; want 400 %q", status, code, pipeline.CodeContentBlocked)
	}
}
