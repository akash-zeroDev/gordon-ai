package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"cyberforge/gateway/internal/config"
	"cyberforge/gateway/internal/mcphttp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() { os.Exit(run()) }
func run() int {
	endpoint := flag.String("url", "http://127.0.0.1:8080/mcp", "loopback MCP endpoint")
	message := flag.String("message", "CYBERFORGE Step 2", "synthetic message to echo")
	tool := flag.String("tool", "demo.echo", "expected exposed tool (echo for direct fixture verification)")
	flag.Parse()
	if err := config.ValidateEndpoint(*endpoint); err != nil {
		fmt.Fprintln(os.Stderr, "invalid local MCP URL")
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	client, transport := mcphttp.Client(5 * time.Second)
	defer transport.CloseIdleConnections()
	session, err := mcphttp.SDKClient("cyberforge-demo-client").Connect(ctx, mcphttp.Transport(*endpoint, client), &mcp.ClientSessionOptions{ProtocolVersion: mcphttp.ProtocolVersion})
	if err != nil {
		fmt.Fprintln(os.Stderr, "MCP connection failed")
		return 1
	}
	defer session.Close()
	if session.InitializeResult().ProtocolVersion != mcphttp.ProtocolVersion {
		fmt.Fprintln(os.Stderr, "MCP protocol mismatch")
		return 1
	}
	found := false
	for definition, err := range session.Tools(ctx, nil) {
		if err != nil {
			fmt.Fprintln(os.Stderr, "tool discovery failed")
			return 1
		}
		if definition.Name == *tool {
			found = true
		}
	}
	if !found {
		fmt.Fprintln(os.Stderr, "expected tool missing")
		return 1
	}
	result, err := session.CallTool(ctx, &mcp.CallToolParams{Name: *tool, Arguments: map[string]string{"message": *message}})
	if err != nil || result == nil || result.IsError || result.NeedsInput() || len(result.Content) != 1 {
		fmt.Fprintln(os.Stderr, "MCP echo failed")
		return 1
	}
	text, ok := result.Content[0].(*mcp.TextContent)
	if !ok || text.Text != *message {
		fmt.Fprintln(os.Stderr, "echo result mismatch")
		return 1
	}
	fmt.Printf("PASS: discovered %s; echo matched; protocol %s\n", *tool, mcphttp.ProtocolVersion)
	return 0
}
