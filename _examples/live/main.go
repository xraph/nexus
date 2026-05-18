// Example: stream from Gemini Live via the BiStream interface.
//
// Run:
//
//	GEMINI_API_KEY=... go run ./_examples/live
//
// Connects to the Live WebSocket API, sends a text turn, and prints text
// + audio deltas. Demonstrates SetupConfig with system_instruction.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"github.com/xraph/nexus/provider"
	geminilive "github.com/xraph/nexus/providers/geminilive"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("GEMINI_API_KEY is required")
	}

	p := geminilive.New(apiKey,
		geminilive.WithModel("models/gemini-2.0-flash-exp"),
		geminilive.WithSetup(geminilive.SetupConfig{
			SystemInstruction: map[string]any{
				"parts": []map[string]any{{"text": "Reply in one short sentence."}},
			},
			GenerationConfig: map[string]any{"temperature": 0.7},
		}),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Messages: []provider.Message{{Role: "user", Content: "Hello, Live API!"}},
	})
	if err != nil {
		return fmt.Errorf("CompleteStream: %w", err)
	}
	defer func() { _ = stream.Close() }()

	if _, ok := stream.(provider.BiStream); ok {
		fmt.Println("[bidi capable]")
	}

	for {
		c, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			fmt.Println()
			return nil
		}
		if err != nil {
			return fmt.Errorf("stream.Next: %w", err)
		}
		switch c.Kind {
		case provider.EventDelta:
			fmt.Print(c.Delta.Content)
		case provider.EventAudio:
			if c.Delta.Audio != nil && c.Delta.Audio.Transcript != "" {
				fmt.Printf("[transcript] %s\n", c.Delta.Audio.Transcript)
			}
		case provider.EventToolCallDelta:
			fmt.Printf("\n[tool call] %s(%s)\n", c.Delta.ToolCalls[0].Function.Name, c.Delta.ToolCalls[0].Function.Arguments)
		case provider.EventError:
			return fmt.Errorf("upstream error: %s", c.Err)
		}
	}
}
