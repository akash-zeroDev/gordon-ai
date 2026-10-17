package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"cyberforge/gateway/internal/mcphttp"
)

type testProcess struct {
	cmd  *exec.Cmd
	logs *logBuffer
	done chan struct{}
	err  error // published by closing done
}

func startProcess(t *testing.T, binary string, env []string, args ...string) *testProcess {
	t.Helper()
	p := &testProcess{cmd: exec.Command(binary, args...), logs: new(logBuffer), done: make(chan struct{})}
	p.cmd.Env = env
	p.cmd.Stdout = p.logs
	p.cmd.Stderr = p.logs
	if err := p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.err = p.cmd.Wait(); close(p.done) }()
	t.Cleanup(func() {
		select {
		case <-p.done:
		default:
			_ = p.cmd.Process.Kill()
			<-p.done
		}
	})
	return p
}

func (p *testProcess) wait(t *testing.T) error {
	t.Helper()
	select {
	case <-p.done:
		return p.err
	case <-time.After(5 * time.Second):
		t.Fatalf("binary did not exit: %s", p.logs.String())
		return nil
	}
}

func cleanEnvironment() []string {
	var env []string
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if strings.HasPrefix(key, "GATEWAY_") || strings.HasPrefix(key, "MCP_") || key == "MCPGODEBUG" {
			continue
		}
		switch key {
		case "LOG_LEVEL", "READ_HEADER_TIMEOUT", "READ_TIMEOUT", "WRITE_TIMEOUT", "IDLE_TIMEOUT", "SHUTDOWN_TIMEOUT":
			continue
		}
		env = append(env, value)
	}
	return env
}

func waitListening(t *testing.T, p *testProcess) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(p.logs.String(), `"msg":"listening"`) {
			return
		}
		select {
		case <-p.done:
			t.Fatalf("startup failed: %s", p.logs.String())
		default:
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("startup did not listen: %s", p.logs.String())
}

func processAddress(t *testing.T) string {
	t.Helper()
	l := listen(t)
	address := l.Addr().String()
	_ = l.Close()
	return address
}

func TestBuiltBinaries(t *testing.T) {
	if testing.Short() {
		t.Skip("process integration requires building binaries")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process signals")
	}
	binaries := map[string]string{}
	for _, name := range []string{"gateway", "demo-upstream", "demo-client"} {
		target := filepath.Join(t.TempDir(), name)
		ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
		command := exec.CommandContext(ctx, "go", "build", "-o", target, "../../cmd/"+name)
		out, err := command.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("build %s: %v\n%s", name, err, out)
		}
		binaries[name] = target
	}
	t.Run("documented_demo", func(t *testing.T) {
		upstreamAddr := processAddress(t)
		upstream := startProcess(t, binaries["demo-upstream"], cleanEnvironment(), "-addr", upstreamAddr)
		waitListening(t, upstream)
		address := processAddress(t)
		_, port, _ := strings.Cut(address, ":")
		env := append(cleanEnvironment(), "GATEWAY_PORT="+port, "MCP_UPSTREAM_URL=http://"+upstreamAddr+"/mcp")
		gateway := startProcess(t, binaries["gateway"], env)
		waitListening(t, gateway)
		for _, target := range []struct{ addr, tool string }{{upstreamAddr, "echo"}, {address, "demo.echo"}} {
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			command := exec.CommandContext(ctx, binaries["demo-client"], "-url", "http://"+target.addr+"/mcp", "-tool", target.tool, "-message", "CYBERFORGE Step 2")
			output, err := command.CombinedOutput()
			cancel()
			if err != nil || !strings.Contains(string(output), "PASS: discovered "+target.tool) {
				t.Fatalf("smoke client: %v %s", err, output)
			}
		}
		for _, path := range []string{"/healthz", "/readyz"} {
			response, err := http.Get("http://" + address + path)
			if err != nil {
				t.Fatal(err)
			}
			response.Body.Close()
			if response.StatusCode != 200 {
				t.Fatalf("%s = %d", path, response.StatusCode)
			}
		}
		occupied := startProcess(t, binaries["gateway"], env)
		if occupied.wait(t) == nil || strings.Contains(occupied.logs.String(), `"msg":"listening"`) {
			t.Fatal("occupied port should fail before listening")
		}
		_ = gateway.cmd.Process.Signal(syscall.SIGTERM)
		if err := gateway.wait(t); err != nil {
			t.Fatal(err, gateway.logs.String())
		}
		select {
		case <-upstream.done:
			t.Fatal("gateway terminated external fixture")
		default:
		}
		_ = upstream.cmd.Process.Signal(os.Interrupt)
		if err := upstream.wait(t); err != nil {
			t.Fatal(err)
		}
		missing := startProcess(t, binaries["gateway"], env)
		if missing.wait(t) == nil || strings.Contains(missing.logs.String(), `"msg":"listening"`) || !strings.Contains(missing.logs.String(), "upstream_connect_failed") {
			t.Fatal("missing upstream should fail startup")
		}
		invalid := startProcess(t, binaries["gateway"], append(cleanEnvironment(), "GATEWAY_HOST=0.0.0.0"))
		if invalid.wait(t) == nil || !strings.Contains(invalid.logs.String(), "GATEWAY_HOST") {
			t.Fatal("invalid configuration should fail startup")
		}
	})
	for _, sig := range []os.Signal{os.Interrupt, syscall.SIGTERM} {
		for _, force := range []bool{false, true} {
			t.Run(fmt.Sprintf("signal=%s/force=%v", sig, force), func(t *testing.T) {
				f := newFixture(t, false)
				address := processAddress(t)
				_, port, _ := strings.Cut(address, ":")
				timeout := "2s"
				if force {
					timeout = "100ms"
				}
				env := append(cleanEnvironment(), "GATEWAY_PORT="+port, "MCP_UPSTREAM_URL="+f.server.URL+"/mcp", "MCP_CALL_TIMEOUT=4s", "SHUTDOWN_TIMEOUT="+timeout)
				p := startProcess(t, binaries["gateway"], env)
				waitListening(t, p)
				client, transport := mcphttp.Client(time.Second)
				defer transport.CloseIdleConnections()
				session, err := mcphttp.SDKClient("process-test").Connect(t.Context(), mcphttp.Transport("http://"+address+"/mcp", client), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				_, _ = session.ListTools(t.Context(), nil)
				done := make(chan error, 1)
				go func() {
					_, err := call(t, session, "demo.wait", map[string]string{"message": "in flight"})
					done <- err
				}()
				await(t, f.started)
				if err := p.cmd.Process.Signal(sig); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(time.Second)
				for !strings.Contains(p.logs.String(), "shutdown_started") && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				if !force {
					f.release <- struct{}{}
				}
				err = p.wait(t)
				if force {
					if err == nil {
						t.Fatal("forced shutdown exited zero")
					}
					await(t, f.cancelled)
					if result(t, done) == nil {
						t.Fatal("forced request succeeded")
					}
				} else {
					if err != nil {
						t.Fatal(err, p.logs.String())
					}
					if err := result(t, done); err != nil {
						t.Fatal(err)
					}
					if !strings.Contains(p.logs.String(), "shutdown_completed") {
						t.Fatal("missing completion log")
					}
				}
				if f.calls.Load() != 1 {
					t.Fatal("invocation replayed")
				}
			})
		}
	}
}
