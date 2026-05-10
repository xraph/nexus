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
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	engine := nexus.NewEngine(nexus.WithProvider(openai.New(apiKey)))

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	srv := grpc.NewServer()
	grpcsrv.Register(srv, engine)

	go func() {
		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		<-sigCh
		fmt.Println("shutting down")
		srv.GracefulStop()
	}()

	fmt.Println("nexus.v1.Completions/CompleteStream listening on :50051")
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("serve: %v", err)
	}
	_ = context.Background()
}
