package mcpbridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

type entry struct {
	upstreamID, original string
	tool                 *mcp.Tool
	input, output        *jsonschema.Schema
}

var validName = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

func catalog(id string, tools []*mcp.Tool) (map[string]*entry, error) {
	if len(tools) == 0 {
		return nil, errors.New("catalog_empty")
	}
	entries := make(map[string]*entry, len(tools))
	for _, tool := range tools {
		if tool == nil || !validName.MatchString(tool.Name) || !validName.MatchString(id+"."+tool.Name) {
			return nil, errors.New("catalog_invalid_name")
		}
		name := id + "." + tool.Name
		if _, exists := entries[name]; exists {
			return nil, errors.New("catalog_duplicate_name")
		}
		// Deep-copy definitions before publication; SDK-owned objects remain untouched.
		data, err := json.Marshal(tool)
		if err != nil {
			return nil, errors.New("catalog_invalid_definition")
		}
		var cloned mcp.Tool
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if decoder.Decode(&cloned) != nil {
			return nil, errors.New("catalog_invalid_definition")
		}
		cloned.Name = name
		cloned.Meta = nil // No unreviewed definition metadata crosses the bridge.
		input, err := resolveSchema(cloned.InputSchema, true)
		if err != nil {
			return nil, err
		}
		var output *jsonschema.Schema
		if cloned.OutputSchema != nil {
			output, err = resolveSchema(cloned.OutputSchema, false)
			if err != nil {
				return nil, err
			}
		}
		entries[name] = &entry{upstreamID: id, original: tool.Name, tool: &cloned, input: input, output: output}
	}
	return entries, nil
}

func resolveSchema(value any, input bool) (*jsonschema.Schema, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, errors.New("catalog_invalid_schema")
	}
	schema, err := jsonschema.UnmarshalJSON(bytes.NewReader(data))
	if err != nil {
		return nil, errors.New("catalog_invalid_schema")
	}
	object, ok := schema.(map[string]any)
	if input && (!ok || object["type"] != "object") {
		return nil, errors.New("catalog_invalid_schema")
	}
	// Never fetch remote or local-file schema references.
	compiler := jsonschema.NewCompiler()
	compiler.UseLoader(rejectSchemaLoader{})
	const location = "https://cyberforge.invalid/tool-schema"
	if err := compiler.AddResource(location, schema); err != nil {
		return nil, errors.New("catalog_invalid_schema")
	}
	resolved, err := compiler.Compile(location)
	if err != nil {
		return nil, errors.New("catalog_unsupported_schema")
	}
	return resolved, nil
}

type rejectSchemaLoader struct{}

func (rejectSchemaLoader) Load(string) (any, error) {
	return nil, errors.New("external_schema_not_supported")
}

func validArguments(raw json.RawMessage, schema *jsonschema.Schema) bool {
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	value, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return false
	}
	_, object := value.(map[string]any)
	return object && schema.Validate(value) == nil
}
