// Example: serve nexus completions over gRPC server-streaming.
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./_examples/grpc
//
// Then point any nexus.v1 client at localhost:50051.
//
// Error contract: when the upstream provider fails mid-stream, the server
// emits a typed StreamEvent{Type: ERROR, Error: {message,type,...}} frame
// AND returns a non-nil error from the streaming RPC. Clients should
// handle both signals: drain pending events, then check Recv() error.
package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"google.golang.org/grpc"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/grpcsrv"
	"github.com/xraph/nexus/providers/openai"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("OPENAI_API_KEY is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	engine := nexus.NewEngine(nexus.WithProvider(openai.New(apiKey)))

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", ":50051")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := grpc.NewServer()
	grpcsrv.Register(srv, engine)

	go func() {
		<-ctx.Done()
		fmt.Println("shutting down")
		srv.GracefulStop()
	}()

	fmt.Println("nexus.v1.Completions/CompleteStream listening on :50051")
	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("serve: %w", err)
	}
	return nil
}
