package mcp

import (
	"context"

	"github.com/xraph/nexus/pipeline"
	"github.com/xraph/nexus/provider"
)

// Middleware integrates MCP tool handling into the Nexus pipeline.
// It intercepts tool call responses and routes them to the MCP server
// for execution, then feeds results back into the conversation.
type Middleware struct {
	server *Server
}

// NewMiddleware creates an MCP pipeline middleware.
func NewMiddleware(server *Server) *Middleware {
	return &Middleware{server: server}
}

func (m *Middleware) Name() string  { return "mcp" }
func (m *Middleware) Priority() int { return 310 } // after routing, before provider call

func (m *Middleware) Process(ctx context.Context, req *pipeline.Request, next pipeline.NextFunc) (*pipeline.Response, error) {
	if req.Completion == nil {
		return next(ctx)
	}

	// Inject available MCP tools into the request
	if len(m.server.tools) > 0 && len(req.Completion.Tools) == 0 {
		for _, t := range m.server.tools {
			req.Completion.Tools = append(req.Completion.Tools, provider.Tool{
				Type: "function",
				Function: provider.ToolFunction{
					Name:        t.Name,
					Description: t.Description,
					Parameters:  t.InputSchema,
				},
			})
		}
	}

	// Execute the pipeline
	resp, err := next(ctx)
	if err != nil {
		return nil, err
	}

	// Check if the response contains tool calls that should be executed
	if resp.Completion != nil && len(resp.Completion.Choices) > 0 {
		choice := resp.Completion.Choices[0]
		if len(choice.Message.ToolCalls) > 0 {
			// Store tool results in response state for the caller to handle
			if resp.Completion.State == nil {
				resp.Completion.State = make(map[string]any)
			}
			resp.Completion.State["mcp_tool_calls"] = choice.Message.ToolCalls
		}
	}

	return resp, nil
}
