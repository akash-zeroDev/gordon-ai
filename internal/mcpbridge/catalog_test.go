package mcpbridge

import (
	"encoding/json"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestCatalogValidationAndSnapshot(t *testing.T) {
	schema := map[string]any{"type": "object", "properties": map[string]any{"n": map[string]any{"type": "integer"}}, "required": []string{"n"}}
	tool := &mcp.Tool{Name: "echo", Description: "original", InputSchema: schema, Annotations: &mcp.ToolAnnotations{ReadOnlyHint: true}}
	entries, err := catalog("demo", []*mcp.Tool{tool})
	if err != nil {
		t.Fatal(err)
	}
	tool.Description = "changed"
	tool.Annotations.ReadOnlyHint = false
	schema["type"] = "string"
	entry := entries["demo.echo"]
	if entry.original != "echo" || entry.upstreamID != "demo" || entry.tool.Description != "original" || !entry.tool.Annotations.ReadOnlyHint {
		t.Fatal("snapshot mutated")
	}
	if !validArguments(json.RawMessage(`{"n":9007199254740993}`), entry.input) {
		t.Fatal("integer precision was lost during validation")
	}
	for _, raw := range []string{`{"n":"bad"}`, `{}`, `null`, `[]`, `42`, `{"n":1.5}`} {
		if validArguments(json.RawMessage(raw), entry.input) {
			t.Fatalf("accepted %s", raw)
		}
	}
	good := &mcp.Tool{Name: "ok", InputSchema: json.RawMessage(`{"type":"object"}`)}
	cases := [][]*mcp.Tool{nil, {nil}, {good, good}, {{Name: "bad/name", InputSchema: good.InputSchema}}, {{Name: "bad", InputSchema: json.RawMessage(`{"type":"array"}`)}}, {{Name: "bad", InputSchema: json.RawMessage(`{"type":"object","properties":{"n":{"$ref":"https://example.com/schema"}}}`)}}}
	for i, tools := range cases {
		if _, err := catalog("demo", tools); err == nil {
			t.Fatalf("accepted invalid catalog %d", i)
		}
	}
}
