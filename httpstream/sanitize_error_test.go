package httpstream_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/pipeline/middlewares"
)

func TestSanitizeError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		err       error
		wantType  string
		retryable bool
	}{
		{"nil returns nil", nil, "", false},
		{"context canceled", context.Canceled, "canceled", false},
		{"deadline", context.DeadlineExceeded, "timeout", true},
		{"generic upstream", errors.New("kaboom"), "upstream", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := httpstream.SanitizeError(c.err, "req-1")
			if c.err == nil {
				if got != nil {
					t.Fatalf("expected nil for nil input, got %+v", got)
				}
				return
			}
			if got == nil {
				t.Fatal("expected non-nil")
			}
			if got.Type != c.wantType {
				t.Fatalf("type = %q, want %q", got.Type, c.wantType)
			}
			if got.Retryable != c.retryable {
				t.Fatalf("retryable = %v, want %v", got.Retryable, c.retryable)
			}
			if got.RequestID != "req-1" {
				t.Fatalf("request_id = %q", got.RequestID)
			}
		})
	}
}

func TestSanitizeError_DoesNotLeakInternalForGeneric(t *testing.T) {
	t.Parallel()
	got := httpstream.SanitizeError(errors.New("/etc/passwd not readable"), "")
	if got == nil || got.Message != "upstream error" || got.Type != "upstream" {
		t.Fatalf("a non-refusal must carry only the fixed message: %+v", got)
	}
}

func TestSanitizeError_ARefusalSaysSo(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		err       error
		code      string
		retryable bool
	}{
		{"rate limited", &pipeline.RefusalError{Code: pipeline.CodeRateLimited, Status: 429, Message: "slow down"}, "rate_limited", true},
		{"forbidden", &pipeline.RefusalError{Code: pipeline.CodeForbidden, Status: 403, Message: "no"}, "forbidden", false},
		{"wrapped guard block", fmt.Errorf("stage: %w", &pipeline.RefusalError{Code: pipeline.CodeContentBlocked, Status: 400, Message: "blocked"}), "content_blocked", false},
		{"stream quota", &middlewares.QuotaError{What: "tokens"}, "quota_exceeded", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := httpstream.SanitizeError(c.err, "req-9")
			var ref pipeline.Refusal
			if !errors.As(c.err, &ref) {
				t.Fatal("test error is not a refusal")
			}
			if got.Type != "refused" || got.Code != c.code || got.Retryable != c.retryable || got.RequestID != "req-9" {
				t.Fatalf("got %+v, want refused/%s retryable=%v", got, c.code, c.retryable)
			}
			if got.Message != ref.Error() {
				t.Fatalf("message = %q, want the refusal's own text %q", got.Message, ref.Error())
			}
		})
	}
}
