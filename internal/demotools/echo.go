// Package demotools provides a harmless fixture with no external side effects.
package demotools

import (
	"context"
	"encoding/json"

	"cyberforge/gateway/internal/mcphttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type EchoInput struct {
	Message string `json:"message"`
}
type EchoOutput struct {
	Message string `json:"message"`
}

func Server() *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{Name: "cyberforge-demo", Version: "0.2.0"}, mcphttp.ServerOptions())
	mcp.AddTool(server, &mcp.Tool{
		Name: "echo", Description: "Return a synthetic message unchanged.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{"message":{"type":"string"}},"required":["message"],"additionalProperties":false}`),
		Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true, IdempotentHint: true},
	}, func(ctx context.Context, req *mcp.CallToolRequest, input EchoInput) (*mcp.CallToolResult, EchoOutput, error) {
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: input.Message}}}, EchoOutput{input.Message}, nil
	})
	return server
}
