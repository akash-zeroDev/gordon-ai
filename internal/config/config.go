// Package config parses the service configuration once at startup.
package config

import (
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Host                string
	Port                int
	LogLevel            slog.Level
	ReadHeaderTimeout   time.Duration
	ReadTimeout         time.Duration
	WriteTimeout        time.Duration
	IdleTimeout         time.Duration
	ShutdownTimeout     time.Duration
	MCPUpstreamID       string
	MCPUpstreamURL      string
	MCPConnectTimeout   time.Duration
	MCPDiscoveryTimeout time.Duration
	MCPCallTimeout      time.Duration
	MCPMaxRequestBytes  int64
}

func (c Config) Address() string { return net.JoinHostPort(c.Host, strconv.Itoa(c.Port)) }

func Load() (Config, error) { return load(os.LookupEnv) }

func load(lookup func(string) (string, bool)) (Config, error) {
	c := Config{}
	value := func(key, fallback string) string {
		if v, ok := lookup(key); ok {
			return v
		}
		return fallback
	}
	c.Host = value("GATEWAY_HOST", "127.0.0.1")
	if ip := net.ParseIP(c.Host); ip == nil || !ip.IsLoopback() {
		return c, fmt.Errorf("GATEWAY_HOST must be a loopback IP address for Step 2")
	}
	port, err := strconv.Atoi(value("GATEWAY_PORT", "8080"))
	if err != nil || port < 1 || port > 65535 {
		return c, fmt.Errorf("GATEWAY_PORT must be an integer from 1 to 65535")
	}
	c.Port = port
	switch value("LOG_LEVEL", "info") {
	case "debug":
		c.LogLevel = slog.LevelDebug
	case "info":
		c.LogLevel = slog.LevelInfo
	case "warn":
		c.LogLevel = slog.LevelWarn
	case "error":
		c.LogLevel = slog.LevelError
	default:
		return c, fmt.Errorf("LOG_LEVEL must be debug, info, warn or error")
	}
	for _, field := range []struct {
		name, fallback string
		target         *time.Duration
	}{
		{"READ_HEADER_TIMEOUT", "5s", &c.ReadHeaderTimeout},
		{"READ_TIMEOUT", "10s", &c.ReadTimeout},
		{"WRITE_TIMEOUT", "15s", &c.WriteTimeout},
		{"IDLE_TIMEOUT", "60s", &c.IdleTimeout},
		{"SHUTDOWN_TIMEOUT", "10s", &c.ShutdownTimeout},
		{"MCP_CONNECT_TIMEOUT", "5s", &c.MCPConnectTimeout},
		{"MCP_DISCOVERY_TIMEOUT", "5s", &c.MCPDiscoveryTimeout},
		{"MCP_CALL_TIMEOUT", "5s", &c.MCPCallTimeout},
	} {
		d, err := time.ParseDuration(value(field.name, field.fallback))
		if err != nil || d <= 0 {
			return c, fmt.Errorf("%s must be a positive Go duration", field.name)
		}
		*field.target = d
	}
	c.MCPUpstreamID = value("MCP_UPSTREAM_ID", "demo")
	if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`).MatchString(c.MCPUpstreamID) {
		return c, fmt.Errorf("MCP_UPSTREAM_ID must contain 1–32 ASCII letters, digits, hyphens or underscores")
	}
	c.MCPUpstreamURL = value("MCP_UPSTREAM_URL", "http://127.0.0.1:9090/mcp")
	if err := ValidateEndpoint(c.MCPUpstreamURL); err != nil {
		return c, fmt.Errorf("MCP_UPSTREAM_URL: %w", err)
	}
	cap, err := strconv.ParseInt(value("MCP_MAX_REQUEST_BYTES", "1048576"), 10, 64)
	if err != nil || cap <= 0 {
		return c, fmt.Errorf("MCP_MAX_REQUEST_BYTES must be a positive integer")
	}
	c.MCPMaxRequestBytes = cap
	// The MCP write deadline includes body-read, execution and response-write budgets.
	if c.ReadTimeout > time.Duration(1<<63-1)-c.MCPCallTimeout || c.ReadTimeout+c.MCPCallTimeout > time.Duration(1<<63-1)-c.WriteTimeout {
		return c, fmt.Errorf("READ_TIMEOUT + MCP_CALL_TIMEOUT + WRITE_TIMEOUT exceeds the supported duration")
	}
	return c, nil
}

// ValidateEndpoint accepts only explicit, credential-free loopback HTTP endpoints.
// DNS, redirects, proxies and public deployment are outside this local milestone.
func ValidateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("must be an absolute loopback HTTP URL with explicit port and path")
	}
	ip := net.ParseIP(u.Hostname())
	port, err := strconv.Atoi(u.Port())
	if u.Scheme != "http" || ip == nil || !ip.IsLoopback() || err != nil || port < 1 || port > 65535 || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.RawFragment != "" || u.Opaque != "" || u.Path == "" || u.Path[0] != '/' {
		return fmt.Errorf("must be an absolute loopback HTTP URL with explicit port/path and no credentials, query or fragment")
	}
	// url.Parse normalizes an empty fragment; reject that marker too.
	if strings.Contains(raw, "#") {
		return fmt.Errorf("fragments are not allowed")
	}
	return nil
}
