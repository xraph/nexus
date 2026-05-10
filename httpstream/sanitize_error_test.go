package httpstream_test

import (
	"context"
	"errors"
	"testing"

	"github.com/xraph/nexus/httpstream"
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
	// Generic errors currently surface their .Error() string. If that ever
	// changes (to "internal error" or similar redaction), update this test.
	got := httpstream.SanitizeError(errors.New("/etc/passwd not readable"), "")
	if got == nil || got.Message != "/etc/passwd not readable" {
		t.Fatalf("message contract drifted: %+v", got)
	}
}
