// Package mcp provides Model Context Protocol support for Nexus.
// It enables tools, resources, and prompts to be exposed as MCP endpoints,
// allowing LLMs to interact with external tools and data sources.
package mcp

import (
	"context"
)

// Config configures MCP server behavior.
type Config struct {
	// Tools are the available MCP tools.
	Tools []Tool `json:"tools,omitempty"`

	// Resources are the available MCP resources.
	Resources []Resource `json:"resources,omitempty"`

	// Prompts are the available MCP prompt templates.
	Prompts []Prompt `json:"prompts,omitempty"`
}

// Tool defines an MCP tool that can be invoked by LLMs.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
	Handler     ToolHandler    `json:"-"`
}

// ToolHandler processes an MCP tool invocation.
type ToolHandler interface {
	Execute(ctx context.Context, input map[string]any) (*ToolResult, error)
}

// ToolHandlerFunc adapts a function to the ToolHandler interface.
type ToolHandlerFunc func(ctx context.Context, input map[string]any) (*ToolResult, error)

func (f ToolHandlerFunc) Execute(ctx context.Context, input map[string]any) (*ToolResult, error) {
	return f(ctx, input)
}

// ToolResult is the result of a tool execution.
type ToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"is_error,omitempty"`
}

// ContentBlock is a piece of tool output content.
type ContentBlock struct {
	Type string `json:"type"` // "text", "image", "resource"
	Text string `json:"text,omitempty"`
}

// Resource defines an MCP resource that can be read by LLMs.
type Resource struct {
	URI         string          `json:"uri"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	MimeType    string          `json:"mime_type,omitempty"`
	Handler     ResourceHandler `json:"-"`
}

// ResourceHandler reads an MCP resource.
type ResourceHandler interface {
	Read(ctx context.Context, uri string) (*ResourceContent, error)
}

// ResourceHandlerFunc adapts a function to the ResourceHandler interface.
type ResourceHandlerFunc func(ctx context.Context, uri string) (*ResourceContent, error)

func (f ResourceHandlerFunc) Read(ctx context.Context, uri string) (*ResourceContent, error) {
	return f(ctx, uri)
}

// ResourceContent is the content of a resource.
type ResourceContent struct {
	URI      string `json:"uri"`
	MimeType string `json:"mime_type,omitempty"`
	Text     string `json:"text,omitempty"`
}

// Prompt defines an MCP prompt template.
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument defines a parameter for a prompt template.
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// Aware marks a provider as supporting MCP tools natively.
// Providers implementing this interface can pass MCP tools directly
// to the LLM without Nexus needing to handle tool execution.
type Aware interface {
	// SupportsMCP returns true if the provider handles MCP tools.
	SupportsMCP() bool

	// SetTools configures the MCP tools available to this provider.
	SetTools(tools []Tool) error
}

// Server manages MCP tools, resources, and prompts.
type Server struct {
	config    *Config
	tools     map[string]Tool
	resources map[string]Resource
}

// NewServer creates a new MCP server.
func NewServer(config *Config) *Server {
	s := &Server{
		config:    config,
		tools:     make(map[string]Tool),
		resources: make(map[string]Resource),
	}

	if config != nil {
		for _, t := range config.Tools {
			s.tools[t.Name] = t
		}
		for _, r := range config.Resources {
			s.resources[r.URI] = r
		}
	}

	return s
}

// RegisterTool adds a tool to the MCP server.
func (s *Server) RegisterTool(tool Tool) {
	s.tools[tool.Name] = tool
}

// RegisterResource adds a resource to the MCP server.
func (s *Server) RegisterResource(resource Resource) {
	s.resources[resource.URI] = resource
}

// ExecuteTool invokes a registered tool.
func (s *Server) ExecuteTool(ctx context.Context, name string, input map[string]any) (*ToolResult, error) {
	tool, ok := s.tools[name]
	if !ok {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "unknown tool: " + name}},
			IsError: true,
		}, nil
	}
	if tool.Handler == nil {
		return &ToolResult{
			Content: []ContentBlock{{Type: "text", Text: "tool has no handler: " + name}},
			IsError: true,
		}, nil
	}
	return tool.Handler.Execute(ctx, input)
}

// ReadResource reads a registered resource.
func (s *Server) ReadResource(ctx context.Context, uri string) (*ResourceContent, error) {
	resource, ok := s.resources[uri]
	if !ok {
		return nil, nil
	}
	if resource.Handler == nil {
		return nil, nil
	}
	return resource.Handler.Read(ctx, uri)
}

// ListTools returns all registered tools.
func (s *Server) ListTools() []Tool {
	tools := make([]Tool, 0, len(s.tools))
	for _, t := range s.tools {
		tools = append(tools, t)
	}
	return tools
}

// ListResources returns all registered resources.
func (s *Server) ListResources() []Resource {
	resources := make([]Resource, 0, len(s.resources))
	for _, r := range s.resources {
		resources = append(resources, r)
	}
	return resources
}
