package auth_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
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

func TestLogServerErrorRedactsCredentialsInURLs(t *testing.T) {
	const secret = "AIzaSECRET123"
	transport := &url.Error{Op: "Post", URL: "https://generativelanguage.googleapis.com/v1beta/models/m:generateContent?alt=sse&key=" + secret, Err: errors.New("connection reset by peer")}
	cases := map[string]error{
		"a *url.Error, wrapped":     fmt.Errorf("gemini: request failed: %w", transport),
		"a URL formatted as text":   errors.New("dial wss://example.test/ws?api_key=" + secret + " failed"),
		"the cause of a 503":        &pipeline.RefusalError{Code: pipeline.CodeUnavailable, Status: 503, Message: "down", Cause: errors.New("GET /x?access_token=" + secret + "&token=" + secret)},
		"an uppercase name, apikey": errors.New("POST https://h.test/p?APIKEY=" + secret),
	}
	for name, err := range cases {
		t.Run(name, func(t *testing.T) {
			l := &lines{}
			auth.LogServerError(context.Background(), l, "/v1/x", err)
			if len(l.got) != 1 {
				t.Fatalf("logged %d lines, want 1", len(l.got))
			}
			if strings.Contains(l.got[0], secret) {
				t.Fatal("the logged line carries the credential")
			}
			if !strings.Contains(l.got[0], "REDACTED") {
				t.Fatal("the logged line does not say what it redacted")
			}
		})
	}
	// Everything else in the URL is kept, so the log still says what failed.
	l := &lines{}
	auth.LogServerError(context.Background(), l, "", fmt.Errorf("gemini: request failed: %w", transport))
	if !strings.Contains(l.got[0], "alt=sse") || !strings.Contains(l.got[0], "generateContent") || !strings.Contains(l.got[0], "connection reset") {
		t.Fatalf("the redacted line lost the rest of the error: %s", strings.ReplaceAll(l.got[0], secret, "<key>"))
	}
}
