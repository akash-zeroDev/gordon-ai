package config

import (
	"log/slog"
	"strings"
	"testing"
	"time"
)

func from(values map[string]string) (Config, error) {
	return load(func(key string) (string, bool) { v, ok := values[key]; return v, ok })
}

func TestDefaults(t *testing.T) {
	c, err := from(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{"127.0.0.1", 8080, slog.LevelInfo, 5 * time.Second, 10 * time.Second, 15 * time.Second, 60 * time.Second, 10 * time.Second, "demo", "http://127.0.0.1:9090/mcp", 5 * time.Second, 5 * time.Second, 5 * time.Second, 1048576}
	if c != want || c.Address() != "127.0.0.1:8080" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestOverrides(t *testing.T) {
	c, err := from(map[string]string{
		"GATEWAY_HOST": "::1", "GATEWAY_PORT": "65535", "LOG_LEVEL": "debug",
		"READ_HEADER_TIMEOUT": "1s", "READ_TIMEOUT": "2s", "WRITE_TIMEOUT": "3s", "IDLE_TIMEOUT": "4s", "SHUTDOWN_TIMEOUT": "500ms",
		"MCP_UPSTREAM_ID": "local_2", "MCP_UPSTREAM_URL": "http://[::1]:9091/mcp", "MCP_CONNECT_TIMEOUT": "2s", "MCP_DISCOVERY_TIMEOUT": "3s", "MCP_CALL_TIMEOUT": "4s", "MCP_MAX_REQUEST_BYTES": "2048",
	})
	if err != nil {
		t.Fatal(err)
	}
	want := Config{"::1", 65535, slog.LevelDebug, time.Second, 2 * time.Second, 3 * time.Second, 4 * time.Second, 500 * time.Millisecond, "local_2", "http://[::1]:9091/mcp", 2 * time.Second, 3 * time.Second, 4 * time.Second, 2048}
	if c != want || c.Address() != "[::1]:65535" {
		t.Fatalf("unexpected overrides: %+v", c)
	}
	for _, level := range []string{"info", "warn", "error"} {
		if _, err := from(map[string]string{"LOG_LEVEL": level}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := from(map[string]string{"GATEWAY_HOST": "127.0.0.2", "GATEWAY_PORT": "1"}); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidSettings(t *testing.T) {
	cases := map[string][]string{
		"GATEWAY_HOST":          {"", "localhost", "127.0.0.999", "[::1]", "0.0.0.0", "::", "192.168.1.1"},
		"GATEWAY_PORT":          {"", "0", "-1", "65536", "abc", "1.5"},
		"LOG_LEVEL":             {"", "verbose", "INFO"},
		"MCP_UPSTREAM_ID":       {"", "has.dot", "has space", "工具", strings.Repeat("a", 33)},
		"MCP_MAX_REQUEST_BYTES": {"", "0", "-1", "1.5", "9999999999999999999999999"},
		"MCP_UPSTREAM_URL":      {"", "/mcp", "http://localhost:9090/mcp", "http://127.0.0.1/mcp", "http://127.0.0.1:0/mcp", "http://127.0.0.1:65536/mcp", "https://127.0.0.1:9090/mcp", "http://127.0.0.1:9090", "http://user:pass@127.0.0.1:9090/mcp", "http://127.0.0.1:9090/mcp?q=secret", "http://127.0.0.1:9090/mcp?", "http://127.0.0.1:9090/mcp#", "http://127.0.0.1:9090/mcp#secret", "http://192.168.1.1:9090/mcp"},
	}
	for _, key := range []string{"READ_HEADER_TIMEOUT", "READ_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT", "SHUTDOWN_TIMEOUT", "MCP_CONNECT_TIMEOUT", "MCP_DISCOVERY_TIMEOUT", "MCP_CALL_TIMEOUT"} {
		cases[key] = []string{"", "0s", "-1s", "ten", "999999999999999999h"}
	}
	for key, values := range cases {
		for _, value := range values {
			t.Run(key+"/"+value, func(t *testing.T) {
				_, err := from(map[string]string{key: value})
				if err == nil || !strings.Contains(err.Error(), key) {
					t.Fatalf("expected field error, got %v", err)
				}
			})
		}
	}
}
