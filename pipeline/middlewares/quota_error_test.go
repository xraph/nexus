package middlewares_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/xraph/nexus/pipeline/middlewares"
)

func TestIsQuotaExceeded(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"random error", errors.New("boom"), false},
		{"direct quota error", &middlewares.QuotaError{What: "duration"}, true},
		{"wrapped", fmt.Errorf("outer: %w", &middlewares.QuotaError{What: "tokens"}), true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := middlewares.IsQuotaExceeded(c.err); got != c.want {
				t.Fatalf("IsQuotaExceeded(%v) = %v, want %v", c.err, got, c.want)
			}
		})
	}
}

func TestQuotaError_Message(t *testing.T) {
	t.Parallel()
	e := &middlewares.QuotaError{What: "output_tokens"}
	if got := e.Error(); got != "nexus: stream quota exceeded: output_tokens" {
		t.Fatalf("Error() = %q", got)
	}
}
