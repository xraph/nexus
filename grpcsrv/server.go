// Package grpcsrv exposes the Nexus completion engine over gRPC. It
// implements a server-streaming endpoint that mirrors the same
// StreamEvent vocabulary the HTTP wire formats use.
//
// Usage:
//
//	srv := grpc.NewServer()
//	grpcsrv.Register(srv, engine)
//	srv.Serve(lis)
//
// Clients invoke nexus.v1.Completions/CompleteStream and receive a stream
// of StreamEvent messages until type=DONE.
package grpcsrv

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc"

	"github.com/xraph/nexus/provider"

	nexusv1 "github.com/xraph/nexus/grpcsrv/proto/nexus/v1"
)

// CompletionStreamer is the minimal engine surface the server uses.
// nexus.Engine satisfies it implicitly.
type CompletionStreamer interface {
	CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error)
}

// Server implements nexusv1.CompletionsServer.
type Server struct {
	nexusv1.UnimplementedCompletionsServer
	engine CompletionStreamer
}

// NewServer wraps a CompletionStreamer (typically *nexus.Engine).
func NewServer(engine CompletionStreamer) *Server { return &Server{engine: engine} }

// Register installs the Server on a grpc.ServiceRegistrar.
func Register(reg grpc.ServiceRegistrar, engine CompletionStreamer) {
	nexusv1.RegisterCompletionsServer(reg, NewServer(engine))
}

// CompleteStream handles a server-streaming completion. Translates each
// inbound StreamChunk to a wire-protocol StreamEvent and emits a final
// DONE event before returning. Mid-stream errors are surfaced as ERROR
// events and then the stream closes — never partial-retried.
//
// Error contract: clients receive errors via TWO channels.
//
//  1. An in-band StreamEvent{Type:ERROR, Error:{...}} frame, useful for
//     UIs that want to display a typed error without losing prior chunks.
//  2. A non-nil error from the streaming RPC's terminal Recv() call —
//     standard gRPC-status semantics for clients that prefer error-as-
//     return-value handling.
//
// Both signals fire for the same underlying failure. Clients should drain
// pending events first, then inspect the Recv() error.
func (s *Server) CompleteStream(req *nexusv1.CompletionRequest, srv nexusv1.Completions_CompleteStreamServer) error {
	if req == nil || req.Model == "" {
		return errors.New("grpcsrv: model is required")
	}

	completionReq := requestFromProto(req)
	stream, err := s.engine.CompleteStream(srv.Context(), completionReq)
	if err != nil {
		return sendErr(srv, err)
	}
	defer func() { _ = stream.Close() }()

	for {
		select {
		case <-srv.Context().Done():
			return srv.Context().Err()
		default:
		}
		chunk, err := stream.Next(srv.Context())
		if errors.Is(err, io.EOF) {
			return srv.Send(&nexusv1.StreamEvent{Type: nexusv1.StreamEvent_DONE})
		}
		if err != nil {
			_ = sendErr(srv, err) //nolint:errcheck // best-effort: connection may already be torn
			return err
		}
		if chunk == nil {
			continue
		}
		if err := srv.Send(eventFromChunk(chunk)); err != nil {
			return err
		}
	}
}

func sendErr(srv nexusv1.Completions_CompleteStreamServer, err error) error {
	we := &nexusv1.WireError{
		Message: err.Error(),
		Type:    "upstream",
	}
	return srv.Send(&nexusv1.StreamEvent{
		Type:  nexusv1.StreamEvent_ERROR,
		Error: we,
	})
}

func requestFromProto(req *nexusv1.CompletionRequest) *provider.CompletionRequest {
	out := &provider.CompletionRequest{
		Model:     req.Model,
		MaxTokens: int(req.MaxTokens),
		Stream:    true, // gRPC surface is streaming-only
	}
	if req.Temperature != nil {
		t := *req.Temperature
		out.Temperature = &t
	}
	for _, m := range req.Messages {
		out.Messages = append(out.Messages, provider.Message{
			Role:    m.Role,
			Content: m.Content,
		})
	}
	return out
}

// clampInt32 clamps a Go int into the proto int32 range. Token counts are
// well below 2³¹, but G115 still flags the conversion — this makes the
// guarantee explicit.
func clampInt32(n int) int32 {
	const maxI32 = int(^uint32(0) >> 1)
	if n > maxI32 {
		return int32(maxI32)
	}
	if n < -maxI32-1 {
		return -1 - int32(maxI32)
	}
	return int32(n)
}

func eventFromChunk(c *provider.StreamChunk) *nexusv1.StreamEvent {
	ev := &nexusv1.StreamEvent{
		Id:           c.ID,
		Model:        c.Model,
		FinishReason: c.FinishReason,
	}
	switch c.Kind {
	case provider.EventReasoning:
		ev.Type = nexusv1.StreamEvent_REASONING
	case provider.EventToolCallDelta:
		ev.Type = nexusv1.StreamEvent_TOOL_CALL
	case provider.EventAudio:
		ev.Type = nexusv1.StreamEvent_AUDIO
	case provider.EventImage:
		ev.Type = nexusv1.StreamEvent_IMAGE
	case provider.EventUsage:
		ev.Type = nexusv1.StreamEvent_USAGE
	case provider.EventError:
		ev.Type = nexusv1.StreamEvent_ERROR
		ev.Error = &nexusv1.WireError{Message: c.Err, Type: "upstream"}
		return ev
	case provider.EventHeartbeat:
		ev.Type = nexusv1.StreamEvent_HEARTBEAT
		return ev
	default:
		ev.Type = nexusv1.StreamEvent_DELTA
	}

	ev.Delta = &nexusv1.Delta{
		Role:      c.Delta.Role,
		Content:   c.Delta.Content,
		Reasoning: c.Delta.Reasoning,
		Refusal:   c.Delta.Refusal,
	}
	for _, tc := range c.Delta.ToolCalls {
		ev.Delta.ToolCalls = append(ev.Delta.ToolCalls, &nexusv1.ToolCall{
			Id:        tc.ID,
			Type:      tc.Type,
			Name:      tc.Function.Name,
			Arguments: tc.Function.Arguments,
		})
	}
	if c.Delta.Audio != nil {
		ev.Delta.Audio = &nexusv1.Audio{
			Format:     c.Delta.Audio.Format,
			SampleRate: clampInt32(c.Delta.Audio.SampleRate),
			Data:       c.Delta.Audio.Data,
			Transcript: c.Delta.Audio.Transcript,
		}
	}
	if c.Delta.Image != nil {
		ev.Delta.Image = &nexusv1.Image{
			MimeType: c.Delta.Image.MimeType,
			Data:     c.Delta.Image.Data,
			Url:      c.Delta.Image.URL,
		}
	}
	if c.Usage != nil {
		ev.Usage = &nexusv1.Usage{
			PromptTokens:     clampInt32(c.Usage.PromptTokens),
			CompletionTokens: clampInt32(c.Usage.CompletionTokens),
			TotalTokens:      clampInt32(c.Usage.TotalTokens),
			ThinkingTokens:   clampInt32(c.Usage.ThinkingTokens),
		}
	}
	return ev
}
