package httpstream_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"syscall"
	"testing"

	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"
)

// goneWriter is a client that has hung up: every body write fails.
type goneWriter struct{ h http.Header }

func (g *goneWriter) Header() http.Header { return g.h }
func (*goneWriter) WriteHeader(int)       {}
func (*goneWriter) Write([]byte) (int, error) {
	return 0, errors.New("write tcp: connection reset by peer")
}

func TestAFailedWriteToTheClientIsMarkedAsTheClientLeaving(t *testing.T) {
	st := testutil.NewFakeStream([]*provider.StreamChunk{{Delta: provider.Delta{Content: "hi"}}}, nil)
	var got error
	httpstream.Run(context.Background(), &goneWriter{h: http.Header{}}, st, httpstream.NewSSEOpenAIEncoder(),
		httpstream.RunOptions{HeartbeatInterval: -1, OnError: func(err error) { got = err }})
	if !errors.Is(got, httpstream.ErrClientWrite) || !httpstream.ClientGone(context.Background(), got) {
		t.Fatalf("OnError got %v; want an ErrClientWrite the hook can skip", got)
	}
}

func TestClientGoneTellsALeavingClientFromAFailure(t *testing.T) {
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	bg := context.Background()
	cases := []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"request context done", canceled, errors.New("anything"), true},
		{"a write to the client failed", bg, fmt.Errorf("%w: boom", httpstream.ErrClientWrite), true},
		{"broken pipe", bg, fmt.Errorf("write: %w", syscall.EPIPE), true},
		{"a provider reset", bg, fmt.Errorf("read: %w", syscall.ECONNRESET), false},
		{"a provider error", bg, errors.New("upstream 502"), false},
	}
	for _, c := range cases {
		if got := httpstream.ClientGone(c.ctx, c.err); got != c.want {
			t.Errorf("%s: ClientGone = %v, want %v", c.name, got, c.want)
		}
	}
}
