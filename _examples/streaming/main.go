// Example: streaming completions via the channel-based Go API.
//
// Demonstrates two consumer styles:
//
//  1. Iterator: idiomatic for text-only consumers — Stream.Next in a loop.
//  2. Channel:  idiomatic for select-based code — provider.NewChannelAdapter
//     publishes typed StreamChunk values that you can multiplex with
//     ctx.Done, timers, or other goroutines.
//
// Plus an Accumulate demo that drains a stream into a final
// CompletionResponse with merged content, reasoning, and tool calls — the
// same shape Provider.Complete would have returned.
//
// Run:
//
//	OPENAI_API_KEY=sk-... go run ./_examples/streaming
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"time"

	nexus "github.com/xraph/nexus"
	"github.com/xraph/nexus/provider"
	"github.com/xraph/nexus/providers/openai"
)

func main() {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		log.Fatal("OPENAI_API_KEY is required")
	}

	engine := nexus.NewEngine(
		nexus.WithProvider(openai.New(apiKey)),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := &provider.CompletionRequest{
		Model: "gpt-4o-mini",
		Messages: []provider.Message{
			{Role: "user", Content: "Write a haiku about Go channels."},
		},
		Stream: true,
	}

	fmt.Println("=== iterator style ===")
	if err := iteratorDemo(ctx, engine, req); err != nil {
		log.Fatalf("iterator: %v", err)
	}

	fmt.Println("\n=== channel style ===")
	if err := channelDemo(ctx, engine, req); err != nil {
		log.Fatalf("channel: %v", err)
	}

	fmt.Println("\n=== accumulator ===")
	if err := accumulateDemo(ctx, engine, req); err != nil {
		log.Fatalf("accumulate: %v", err)
	}
}

func iteratorDemo(ctx context.Context, engine *nexus.Engine, req *provider.CompletionRequest) error {
	stream, err := engine.CompleteStream(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	for {
		chunk, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			fmt.Println()
			return nil
		}
		if err != nil {
			return err
		}
		switch chunk.Kind {
		case provider.EventReasoning:
			fmt.Printf("[reasoning] %s", chunk.Delta.Reasoning)
		case provider.EventDelta:
			fmt.Print(chunk.Delta.Content)
		}
	}
}

func channelDemo(ctx context.Context, engine *nexus.Engine, req *provider.CompletionRequest) error {
	stream, err := engine.CompleteStream(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	events := provider.NewChannelAdapter(ctx, stream, provider.ChannelOptions{Buffer: 32})

	timeout := time.After(20 * time.Second)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return fmt.Errorf("stream timeout")
		case ev, ok := <-events:
			if !ok {
				fmt.Println()
				return nil
			}
			switch ev.Kind {
			case provider.EventReasoning:
				fmt.Printf("[reasoning] %s", ev.Delta.Reasoning)
			case provider.EventDelta:
				fmt.Print(ev.Delta.Content)
			case provider.EventError:
				return fmt.Errorf("stream error: %s", ev.Err)
			}
		}
	}
}

func accumulateDemo(ctx context.Context, engine *nexus.Engine, req *provider.CompletionRequest) error {
	stream, err := engine.CompleteStream(ctx, req)
	if err != nil {
		return err
	}
	defer func() { _ = stream.Close() }()

	// Accumulate drains the stream and produces the merged response — the
	// same shape Provider.Complete would have returned.
	resp, err := provider.Accumulate(ctx, stream)
	if err != nil {
		return err
	}
	fmt.Printf("merged content: %v\n", resp.Choices[0].Message.Content)
	fmt.Printf("usage: prompt=%d completion=%d total=%d\n",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
	return nil
}
