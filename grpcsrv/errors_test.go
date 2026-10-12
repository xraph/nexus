package grpcsrv_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/xraph/nexus/grpcsrv"
	nexusv1 "github.com/xraph/nexus/grpcsrv/proto/nexus/v1"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

const secret = "AIzaSECRET123"

type lines struct {
	mu  sync.Mutex
	got []string
}

func (l *lines) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.got = append(l.got, msg+" "+fmt.Sprint(args...))
}

func (l *lines) all() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Join(l.got, "\n")
}

// shown prints an ERROR event with the test key hidden.
func shown(we *nexusv1.WireError) string {
	if we == nil {
		return "none"
	}
	return strings.ReplaceAll(fmt.Sprintf("type %q code %q message %q", we.Type, we.Code, we.Message), secret, "<key>")
}

// failingStream fails on its first Next.
type failingStream struct{ err error }

func (f failingStream) Next(context.Context) (*provider.StreamChunk, error) { return nil, f.err }
func (failingStream) Close() error                                          { return nil }
func (failingStream) Usage() *provider.Usage                                { return nil }

type midwayStreamer struct{ err error }

func (m midwayStreamer) CompleteStream(context.Context, *provider.CompletionRequest) (provider.Stream, error) {
	return failingStream(m), nil
}

// call runs one CompleteStream and returns the ERROR event and the final
// Recv error.
func call(t *testing.T, s grpcsrv.CompletionStreamer, log *lines) (*nexusv1.WireError, error) {
	t.Helper()
	srv := grpc.NewServer()
	grpcsrv.Register(srv, s, grpcsrv.WithLogger(log))
	client := nexusv1.NewCompletionsClient(dialBuf(t, srv))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := client.CompleteStream(ctx, &nexusv1.CompletionRequest{Model: "m"})
	if err != nil {
		t.Fatal(err)
	}
	var we *nexusv1.WireError
	for {
		ev, err := stream.Recv()
		if err != nil {
			return we, err
		}
		if ev.Type == nexusv1.StreamEvent_ERROR {
			we = ev.Error
		}
	}
}

func TestAnUpstreamErrorReachesTheClientSanitizedAndTheLogRedacted(t *testing.T) {
	leak := fmt.Errorf("gemini: request failed: Post %q: connection reset", "https://g.test/v1beta/models/m:generateContent?key="+secret)
	for name, s := range map[string]grpcsrv.CompletionStreamer{
		"at the start": &fakeStreamer{err: leak},
		"midway":       midwayStreamer{err: leak},
	} {
		t.Run(name, func(t *testing.T) {
			log := &lines{}
			we, err := call(t, s, log)
			if we == nil || we.Message != "upstream error" || we.Type != "upstream" || strings.Contains(we.Message, secret) {
				t.Fatalf("ERROR event = %s; want the fixed upstream error", shown(we))
			}
			st, _ := status.FromError(err)
			if st.Code() != codes.Internal || st.Message() != "internal error" {
				t.Fatalf("status = %s %q; want Internal, internal error", st.Code(), strings.ReplaceAll(st.Message(), secret, "<key>"))
			}
			logged := log.all()
			if strings.Contains(logged, secret) || !strings.Contains(logged, "connection reset") || !strings.Contains(logged, "REDACTED") {
				t.Fatalf("log = %q; want the cause, with the key redacted", strings.ReplaceAll(logged, secret, "<key>"))
			}
		})
	}
}

func TestARefusalKeepsItsCodeOverGRPC(t *testing.T) {
	cases := []struct {
		status int
		code   string
		want   codes.Code
	}{
		{401, pipeline.CodeUnauthenticated, codes.Unauthenticated},
		{403, pipeline.CodeForbidden, codes.PermissionDenied},
		{400, pipeline.CodeInvalidRequest, codes.InvalidArgument},
		{429, pipeline.CodeRateLimited, codes.ResourceExhausted},
		{503, pipeline.CodeUnavailable, codes.Unavailable},
		{500, "internal_error", codes.Internal},
	}
	for _, c := range cases {
		t.Run(c.code, func(t *testing.T) {
			ref := &pipeline.RefusalError{Code: c.code, Status: c.status, Message: "said no", Cause: errors.New("db at postgres://u:" + secret + "@h")}
			we, err := call(t, &fakeStreamer{err: fmt.Errorf("wrapped: %w", ref)}, &lines{})
			st, _ := status.FromError(err)
			if st.Code() != c.want {
				t.Fatalf("status = %s, want %s", st.Code(), c.want)
			}
			if strings.Contains(st.Message(), secret) || strings.Contains(st.Message(), "wrapped") {
				t.Fatalf("status message %q carries more than the refusal's own text", strings.ReplaceAll(st.Message(), secret, "<key>"))
			}
			if c.want != codes.Internal && (we == nil || we.Code != c.code || we.Type != "refused") {
				t.Fatalf("ERROR event = %s; want type refused, code %s", shown(we), c.code)
			}
		})
	}
}
