package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/pipeline"
)

type lines struct{ got []string }

func (l *lines) Error(msg string, args ...any) {
	l.got = append(l.got, msg+" "+fmt.Sprint(args...))
}

func TestRequestIDIsTheGatewaysOwn(t *testing.T) {
	var inCtx string
	h := auth.RequestID()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { inCtx = pipeline.RequestID(r.Context()) }))
	rec := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/", nil)
	req.Header.Set("X-Request-Id", "req_from_client")
	h.ServeHTTP(rec, req)
	hdr := rec.Header().Get("X-Request-Id")
	if hdr == "" || hdr == "req_from_client" || hdr != inCtx || !strings.HasPrefix(hdr, "req_") {
		t.Fatalf("header %q, context %q: want one gateway-made req_ id in both", hdr, inCtx)
	}
}

func TestLogServerErrorLogsOnlyWhatTheClientIsNotTold(t *testing.T) {
	ctx := pipeline.WithRequestID(context.Background(), "req_1")
	cases := []struct {
		name string
		err  error
		want []string // substrings of the one logged line, nil for no line
	}{
		{"plain error", errors.New("boom"), []string{"req_1", "boom"}},
		{"503 with a cause", &pipeline.RefusalError{Code: pipeline.CodeUnavailable, Status: 503, Message: "down", Cause: errors.New("dsn leak")}, []string{"req_1", "dsn leak"}},
		{"401", &pipeline.RefusalError{Code: pipeline.CodeUnauthenticated, Status: 401, Message: "no"}, nil},
		{"429", &pipeline.RefusalError{Code: pipeline.CodeRateLimited, Status: 429, Message: "slow"}, nil},
		{"client left", context.Canceled, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			l := &lines{}
			auth.LogServerError(ctx, l, "/v1/x", c.err)
			if c.want == nil {
				if len(l.got) != 0 {
					t.Fatalf("logged %v, want nothing", l.got)
				}
				return
			}
			if len(l.got) != 1 {
				t.Fatalf("logged %d lines, want 1", len(l.got))
			}
			for _, w := range c.want {
				if !strings.Contains(l.got[0], w) {
					t.Fatalf("line %q lacks %q", l.got[0], w)
				}
			}
		})
	}
}
