// Example: using Nexus as a Forge extension.
//
// This demonstrates integrating Nexus into a Forge application
// where it auto-discovers Chronicle, Shield, and other ecosystem
// extensions from the DI container.
//
//	go run ./_examples/forge/main.go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/xraph/nexus/extension"
	"github.com/xraph/nexus/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY environment variable is required")
	}

	// Create the Nexus Forge extension.
	nexusExt := extension.New(
		extension.WithProvider(openai.New(apiKey)),
		extension.WithBasePath("/ai"),
	)

	// In a real Forge application, you would register the extension:
	//
	//   app := forge.New(
	//       forge.WithName("my-ai-platform"),
	//       forge.WithExtension(nexusExt),
	//       forge.WithExtension(chronicle.New(db)),
	//       forge.WithExtension(shield.New()),
	//   )
	//   app.Start(ctx)
	//
	// Nexus will auto-discover Chronicle (for audit logging),
	// Shield (for content safety), Relay (for webhooks), etc.

	fmt.Printf("Nexus Forge Extension: %s v%s\n", nexusExt.Name(), nexusExt.Version())
	fmt.Printf("Description: %s\n", nexusExt.Description())
	fmt.Println("\nTo use in a Forge app, register with forge.WithExtension(nexusExt)")
}
