package openairealtime

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/coder/websocket"

	"github.com/xraph/nexus/provider"
)

// realtimeStream is the BiStream wrapper around an OpenAI Realtime
// WebSocket session. It implements both Stream.Next (server → client
// translation) and BiStream.Send (client → server forwarding).
type realtimeStream struct {
	conn      wsConn
	ctx       context.Context
	model     string
	usage     *provider.Usage
	done      bool
	closeMu   sync.Mutex
	closed    bool
	sessionMu sync.RWMutex
	sessionID string
}

func newRealtimeStream(ctx context.Context, conn wsConn, model string, _ *provider.CompletionRequest) *realtimeStream {
	return &realtimeStream{
		conn:  conn,
		ctx:   ctx,
		model: model,
	}
}

// SessionID returns the upstream Realtime session identifier once the
// server has sent session.created. Returns "" before that event arrives.
func (s *realtimeStream) SessionID() string {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return s.sessionID
}

// Next reads the next inbound event and translates it to a StreamChunk.
// Loops past upstream events that don't surface client-visible content
// (heartbeats, session.updated acks, etc.).
func (s *realtimeStream) Next(ctx context.Context) (*provider.StreamChunk, error) {
	if s.done {
		return nil, io.EOF
	}
	for {
		_, data, err := s.conn.Read(ctx)
		if err != nil {
			if isCloseErr(err) {
				s.done = true
				return nil, io.EOF
			}
			return nil, fmt.Errorf("openairealtime: read: %w", err)
		}

		var env serverEvent
		if err := json.Unmarshal(data, &env); err != nil {
			continue
		}

		chunk, terminate := s.translate(&env, data)
		if chunk != nil {
			return chunk, nil
		}
		if terminate {
			s.done = true
			return nil, io.EOF
		}
	}
}

// Close ends the upstream session. Idempotent.
func (s *realtimeStream) Close() error {
	s.closeMu.Lock()
	defer s.closeMu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	s.done = true
	return s.conn.Close(websocket.StatusNormalClosure, "")
}

// Usage returns the most recent token usage frame (set by response.done).
func (s *realtimeStream) Usage() *provider.Usage { return s.usage }

// Send forwards a client envelope to the upstream Realtime API. Translates
// the unified ClientEvent to the appropriate OpenAI event type.
func (s *realtimeStream) Send(ctx context.Context, evt provider.ClientEvent) error {
	var msg any
	switch evt.Type {
	case "audio_chunk":
		if evt.Audio == nil {
			return errors.New("openairealtime: audio_chunk requires Audio payload")
		}
		msg = map[string]any{
			"type":  "input_audio_buffer.append",
			"audio": base64.StdEncoding.EncodeToString(evt.Audio.Data),
		}
	case "image":
		if evt.Image == nil {
			return errors.New("openairealtime: image requires Image payload")
		}
		// Realtime accepts images via the conversation.item.create path
		// using an input_image content block.
		msg = map[string]any{
			"type": "conversation.item.create",
			"item": map[string]any{
				"type": "message",
				"role": "user",
				"content": []map[string]any{{
					"type":      "input_image",
					"mime_type": evt.Image.MimeType,
					"data":      base64.StdEncoding.EncodeToString(evt.Image.Data),
				}},
			},
		}
	case "commit":
		msg = map[string]any{"type": "input_audio_buffer.commit"}
	case "cancel":
		msg = map[string]any{"type": "response.cancel"}
	case "control":
		msg = evt.Data
	default:
		return fmt.Errorf("openairealtime: unsupported client event %q", evt.Type)
	}

	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return s.conn.Write(ctx, websocket.MessageText, body)
}

// translate maps a Realtime server event to a nexus StreamChunk. Returns
// (nil, true) when the upstream signaled session end.
func (s *realtimeStream) translate(env *serverEvent, raw []byte) (*provider.StreamChunk, bool) {
	switch env.Type {
	case "session.created", "session.updated":
		if env.Session != nil && env.Session.ID != "" {
			s.sessionMu.Lock()
			s.sessionID = env.Session.ID
			s.sessionMu.Unlock()
		}
		// Surface session start as a MessageStart frame so consumers that
		// route on Kind can detect it. Plain iterators see an empty delta
		// and skip naturally.
		return &provider.StreamChunk{
			ID:       env.Session.idOrEmpty(),
			Provider: "openai-realtime",
			Model:    s.model,
			Kind:     provider.EventMessageStart,
		}, false

	case "input_audio_buffer.speech_started",
		"input_audio_buffer.speech_stopped",
		"input_audio_buffer.committed",
		"conversation.item.created",
		"conversation.item.input_audio_transcription.completed",
		"rate_limits.updated",
		"response.created",
		"response.output_item.added",
		"response.output_item.done",
		"response.content_part.added",
		"response.content_part.done",
		"response.audio.done",
		"response.audio_transcript.done",
		"response.text.done",
		"response.function_call_arguments.done":
		// Lifecycle events that carry no client-visible delta. Drop.
		return nil, false

	case "response.text.delta":
		return &provider.StreamChunk{
			ID:       env.ResponseID,
			Provider: "openai-realtime",
			Model:    s.model,
			Delta:    provider.Delta{Content: env.Delta},
		}, false

	case "response.audio.delta":
		audioBytes, err := base64.StdEncoding.DecodeString(env.Delta)
		if err != nil {
			return nil, false
		}
		return &provider.StreamChunk{
			ID:       env.ResponseID,
			Provider: "openai-realtime",
			Model:    s.model,
			Kind:     provider.EventAudio,
			Delta: provider.Delta{
				Audio: &provider.AudioChunk{
					Format:     "pcm16",
					SampleRate: 24000,
					Data:       audioBytes,
				},
			},
		}, false

	case "response.audio_transcript.delta":
		return &provider.StreamChunk{
			ID:       env.ResponseID,
			Provider: "openai-realtime",
			Model:    s.model,
			Kind:     provider.EventAudio,
			Delta:    provider.Delta{Audio: &provider.AudioChunk{Transcript: env.Delta}},
		}, false

	case "response.function_call_arguments.delta":
		// Tool-call argument fragment. Realtime carries call_id at the
		// envelope level; index defaults to 0 since the API only exposes
		// one tool slot per response output item.
		return &provider.StreamChunk{
			ID:       env.ResponseID,
			Provider: "openai-realtime",
			Model:    s.model,
			Kind:     provider.EventToolCallDelta,
			Delta: provider.Delta{
				ToolCalls: []provider.ToolCall{{
					ID:   env.CallID,
					Type: "function",
					Function: provider.ToolCallFunc{
						Name:      env.Name,
						Arguments: env.Delta,
					},
				}},
			},
		}, false

	case "response.done":
		// Final usage + finish.
		if env.Response.Usage != nil {
			s.usage = &provider.Usage{
				PromptTokens:     env.Response.Usage.InputTokens,
				CompletionTokens: env.Response.Usage.OutputTokens,
				TotalTokens:      env.Response.Usage.TotalTokens,
			}
		}
		return &provider.StreamChunk{
			ID:           env.ResponseID,
			Provider:     "openai-realtime",
			Model:        s.model,
			FinishReason: "stop",
			Usage:        s.usage,
		}, true

	case "error":
		var errMsg string
		var raw2 struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &raw2) //nolint:errcheck // best-effort
		errMsg = raw2.Error.Message
		if errMsg == "" {
			errMsg = "openairealtime: upstream error"
		}
		return &provider.StreamChunk{
			Provider: "openai-realtime",
			Model:    s.model,
			Kind:     provider.EventError,
			Err:      errMsg,
		}, false

	default:
		return nil, false
	}
}

// serverEvent is the envelope shape used by OpenAI Realtime server events.
// We only decode the fields that drive translate() — provider-specific
// metadata is preserved in the raw bytes for the error path.
type serverEvent struct {
	Type       string         `json:"type"`
	ResponseID string         `json:"response_id,omitempty"`
	CallID     string         `json:"call_id,omitempty"`
	Name       string         `json:"name,omitempty"`
	Delta      string         `json:"delta,omitempty"`
	Session    *sessionFields `json:"session,omitempty"`
	Response   struct {
		Usage *struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
			TotalTokens  int `json:"total_tokens"`
		} `json:"usage,omitempty"`
	} `json:"response,omitempty"`
}

type sessionFields struct {
	ID    string `json:"id,omitempty"`
	Model string `json:"model,omitempty"`
}

func (s *sessionFields) idOrEmpty() string {
	if s == nil {
		return ""
	}
	return s.ID
}

func isCloseErr(err error) bool {
	if err == nil {
		return false
	}
	var ce websocket.CloseError
	return errors.As(err, &ce)
}

// Compile-time checks.
var (
	_ provider.Stream   = (*realtimeStream)(nil)
	_ provider.BiStream = (*realtimeStream)(nil)
)
