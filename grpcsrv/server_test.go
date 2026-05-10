package grpcsrv_test

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"

	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/testutil"

	"github.com/xraph/nexus/grpcsrv"
	nexusv1 "github.com/xraph/nexus/grpcsrv/proto/nexus/v1"
)

type fakeStreamer struct {
	chunks []*provider.StreamChunk
	err    error
}

func (f *fakeStreamer) CompleteStream(_ context.Context, _ *provider.CompletionRequest) (provider.Stream, error) {
	if f.err != nil {
		return nil, f.err
	}
	return testutil.NewFakeStream(f.chunks, nil), nil
}

func dialBuf(t *testing.T, srv *grpc.Server) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1024 * 1024)
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)

	conn, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithContextDialer(func(_ context.Context, _ string) (net.Conn, error) { return lis.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestServer_HappyPath(t *testing.T) {
	t.Parallel()

	chunks := []*provider.StreamChunk{
		{ID: "id-1", Provider: "test", Model: "m", Delta: provider.Delta{Role: "assistant", Content: "Hello"}},
		{Delta: provider.Delta{Content: ", world"}, FinishReason: "stop"},
		{Kind: provider.EventUsage, Usage: &provider.Usage{TotalTokens: 7}},
	}
	streamer := &fakeStreamer{chunks: chunks}
	srv := grpc.NewServer()
	grpcsrv.Register(srv, streamer)

	conn := dialBuf(t, srv)
	client := nexusv1.NewCompletionsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	stream, err := client.CompleteStream(ctx, &nexusv1.CompletionRequest{
		Model:    "m",
		Messages: []*nexusv1.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}

	var combined string
	var sawDone bool
	var lastUsage *nexusv1.Usage
	for {
		ev, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("recv: %v", err)
		}
		switch ev.Type {
		case nexusv1.StreamEvent_DELTA:
			if ev.Delta != nil {
				combined += ev.Delta.Content
			}
		case nexusv1.StreamEvent_USAGE:
			lastUsage = ev.Usage
		case nexusv1.StreamEvent_DONE:
			sawDone = true
		}
	}
	if combined != "Hello, world" {
		t.Fatalf("content = %q", combined)
	}
	if !sawDone {
		t.Fatal("no DONE event")
	}
	if lastUsage == nil || lastUsage.TotalTokens != 7 {
		t.Fatalf("usage missing/wrong: %+v", lastUsage)
	}
}

func TestServer_RejectsMissingModel(t *testing.T) {
	t.Parallel()

	srv := grpc.NewServer()
	grpcsrv.Register(srv, &fakeStreamer{})

	conn := dialBuf(t, srv)
	client := nexusv1.NewCompletionsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.CompleteStream(ctx, &nexusv1.CompletionRequest{})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	if _, err := stream.Recv(); err == nil {
		t.Fatal("expected error from server, got nil")
	}
}

func TestServer_StreamErrorTranslated(t *testing.T) {
	t.Parallel()

	streamer := &fakeStreamer{err: errors.New("upstream broke")}
	srv := grpc.NewServer()
	grpcsrv.Register(srv, streamer)

	conn := dialBuf(t, srv)
	client := nexusv1.NewCompletionsClient(conn)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	stream, err := client.CompleteStream(ctx, &nexusv1.CompletionRequest{Model: "m"})
	if err != nil {
		t.Fatalf("CompleteStream: %v", err)
	}
	ev, err := stream.Recv()
	// Either the server sent an ERROR event then closed, or grpc surfaces
	// the error directly. Both are acceptable.
	if err == nil && ev != nil && ev.Type == nexusv1.StreamEvent_ERROR {
		if ev.Error == nil || ev.Error.Message == "" {
			t.Fatalf("error envelope missing: %+v", ev.Error)
		}
		return
	}
	if err == nil {
		t.Fatalf("expected error event or recv error, got %+v", ev)
	}
}
