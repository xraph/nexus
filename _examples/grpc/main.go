// Example: serve nexus completions over gRPC server-streaming.
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./_examples/grpc
//
// Then point any nexus.v1 client at localhost:50051, with the gateway key the
// example prints at startup in the "x-api-key" metadata (or
// "authorization: Bearer nxs_..."). The in-memory store forgets the key on
// restart. Without the grpcsrv.KeyAuth interceptor the gRPC surface would be
// anonymous: Register does not authenticate.
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
	"github.com/xraph/nexus/key"
	"github.com/xraph/nexus/providers/openai"
	"github.com/xraph/nexus/tenant"
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
	if engine == nil {
		return fmt.Errorf("the gateway did not initialize")
	}

	// KeyAuth refuses a call without a gateway key, so make one. The tenant
	// and key live in the gateway's in-memory store.
	gw := engine.Gateway()
	t, err := gw.Tenants().Create(ctx, &tenant.CreateInput{Name: "Example", Slug: "example"})
	if err != nil {
		return fmt.Errorf("create tenant: %w", err)
	}
	_, rawKey, err := gw.Keys().Create(ctx, &key.CreateInput{TenantID: t.ID.String(), Name: "example"})
	if err != nil {
		return fmt.Errorf("create key: %w", err)
	}
	// Examples may print the key. It is shown once; never log a key in a real
	// service.
	fmt.Println("Gateway key (shown once):", rawKey)

	var lc net.ListenConfig
	lis, err := lc.Listen(ctx, "tcp", ":50051")
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	srv := grpc.NewServer(grpc.StreamInterceptor(grpcsrv.KeyAuth(gw.Keys(), gw.Tenants())))
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
