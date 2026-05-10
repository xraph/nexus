// Example: stream from OpenAI Realtime via the BiStream interface.
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./_examples/realtime
//
// This connects to api.openai.com's Realtime WebSocket, asks for a short
// text response, and prints text deltas + audio transcripts as they arrive.
// Bidirectional audio input is demonstrated via Send (the example doesn't
// open a microphone — you'd plug a portaudio/oboe stream into Send in a
// real app).
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
	openairealtime "github.com/xraph/nexus/providers/openairealtime"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	p := openairealtime.New(apiKey, openairealtime.WithModel("gpt-4o-realtime-preview"))

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	stream, err := p.CompleteStream(ctx, &provider.CompletionRequest{
		Model: "gpt-4o-realtime-preview",
		Messages: []provider.Message{
			{Role: "user", Content: "Say a haiku about Go channels in 3 lines."},
		},
	})
	if err != nil {
		log.Fatalf("CompleteStream: %v", err)
	}
	defer func() { _ = stream.Close() }()

	// Bidirectional capability — caller can pipe microphone audio upstream:
	if bi, ok := stream.(provider.BiStream); ok {
		fmt.Println("[bidi capable — bi.Send is wired]")
		_ = bi // production app: pump audio frames here
	}

	for {
		c, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			fmt.Println()
			return
		}
		if err != nil {
			log.Fatalf("Next: %v", err)
		}
		switch c.Kind {
		case provider.EventDelta:
			fmt.Print(c.Delta.Content)
		case provider.EventAudio:
			if c.Delta.Audio != nil && c.Delta.Audio.Transcript != "" {
				fmt.Printf("[transcript] %s\n", c.Delta.Audio.Transcript)
			}
		case provider.EventError:
			log.Fatalf("upstream error: %s", c.Err)
		}
	}
}
