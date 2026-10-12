// Package grpcsrv exposes the Nexus completion engine over gRPC. It
// implements a server-streaming endpoint that mirrors the same
// StreamEvent vocabulary the HTTP wire formats use.
//
// Usage:
//
//	srv := grpc.NewServer(grpc.StreamInterceptor(grpcsrv.KeyAuth(gw.Keys(), gw.Tenants())))
//	grpcsrv.Register(srv, gw.Engine())
//	srv.Serve(lis)
//
// Clients invoke nexus.v1.Completions/CompleteStream and receive a stream
// of StreamEvent messages until type=DONE.
//
// Warning: Register does not authenticate. Without the KeyAuth interceptor
// the gRPC surface is anonymous, whatever Config.RequireAPIKey says (that
// setting covers the HTTP api and proxy routes only). Every call is then
// unattributed, so no tenant budget, RPM, TPM or scope applies, and only
// GlobalRateLimit limits it.
package grpcsrv

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/xraph/nexus/auth"
	"github.com/xraph/nexus/httpstream"
	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"

	nexusv1 "github.com/xraph/nexus/grpcsrv/proto/nexus/v1"
)

// method names the RPC in the log.
const method = "/nexus.v1.Completions/CompleteStream"

// CompletionStreamer is the minimal engine surface the server uses.
// nexus.Engine satisfies it implicitly.
type CompletionStreamer interface {
	CompleteStream(ctx context.Context, req *provider.CompletionRequest) (provider.Stream, error)
}

// Server implements nexusv1.CompletionsServer.
type Server struct {
	nexusv1.UnimplementedCompletionsServer
	engine CompletionStreamer
	log    auth.Logger
}

// Option configures the Server.
type Option func(*Server)

// WithLogger sets where the cause of a failed stream is logged. The default
// is slog.Default(). nexus.Logger satisfies auth.Logger.
func WithLogger(l auth.Logger) Option { return func(s *Server) { s.log = l } }

// NewServer wraps a CompletionStreamer (typically *nexus.Engine).
func NewServer(engine CompletionStreamer, opts ...Option) *Server {
	s := &Server{engine: engine, log: slog.Default()}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Register installs the Server on a grpc.ServiceRegistrar. It does not
// authenticate: install KeyAuth as the server's stream interceptor, or the
// surface is anonymous and only GlobalRateLimit applies to it.
func Register(reg grpc.ServiceRegistrar, engine CompletionStreamer, opts ...Option) {
	nexusv1.RegisterCompletionsServer(reg, NewServer(engine, opts...))
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
//
// Both carry only what an HTTP client would see: a refusal's own code and
// text, or a fixed message for anything else. The gRPC status code follows
// the refusal (Unauthenticated, PermissionDenied, InvalidArgument,
// ResourceExhausted, Unavailable), and anything that is not a refusal is
// Internal with "internal error". The real error goes to the server's
// logger, never to the client.
func (s *Server) CompleteStream(req *nexusv1.CompletionRequest, srv nexusv1.Completions_CompleteStreamServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}
	// An empty model is not refused here: the access stage fills the
	// tenant's default model, and refuses the request when there is none.

	completionReq := requestFromProto(req)
	stream, err := s.engine.CompleteStream(srv.Context(), completionReq)
	if err != nil {
		return s.fail(srv, err)
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
			return s.fail(srv, err)
		}
		if chunk == nil {
			continue
		}
		if err := srv.Send(eventFromChunk(chunk)); err != nil {
			return err
		}
	}
}

// fail logs err, sends the sanitized ERROR event (best effort: the
// connection may already be torn) and returns the matching gRPC status.
func (s *Server) fail(srv nexusv1.Completions_CompleteStreamServer, err error) error {
	ctx := srv.Context()
	auth.LogServerError(ctx, s.log, method, err)
	we := httpstream.SanitizeError(err, pipeline.RequestID(ctx))
	_ = srv.Send(&nexusv1.StreamEvent{ //nolint:errcheck // best-effort: connection may already be torn
		Type: nexusv1.StreamEvent_ERROR,
		Error: &nexusv1.WireError{
			Message: we.Message, Type: we.Type, Code: we.Code,
			Retryable: we.Retryable, RequestId: we.RequestID,
		},
	})
	return statusOf(err)
}

// statusOf maps err to the gRPC status a client sees. Only a refusal's own
// text, or a cancel's, is ever put in it.
func statusOf(err error) error {
	switch {
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "request timed out")
	}
	var ref pipeline.Refusal
	if !errors.As(err, &ref) {
		return status.Error(codes.Internal, "internal error")
	}
	switch ref.StatusCode() {
	case http.StatusUnauthorized:
		return status.Error(codes.Unauthenticated, ref.Error())
	case http.StatusForbidden:
		return status.Error(codes.PermissionDenied, ref.Error())
	case http.StatusBadRequest:
		return status.Error(codes.InvalidArgument, ref.Error())
	case http.StatusTooManyRequests:
		return status.Error(codes.ResourceExhausted, ref.Error())
	case http.StatusServiceUnavailable:
		return status.Error(codes.Unavailable, ref.Error())
	}
	return status.Error(codes.Internal, "internal error")
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
