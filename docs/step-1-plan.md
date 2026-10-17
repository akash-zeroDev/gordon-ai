# Step 1 — Set Up the Go Gateway

Source: https://app.notion.com/p/3fcc64d5199681df9f08c9bce2b47281?pvs=204

Local snapshot of the Notion implementation plan, retrieved 2026-10-17. The source checklist describes planned work; see README.md for implementation and verification.

**Purpose:** Build a runnable, configurable Go service that later becomes the GuardMCP enforcement gateway.
**Status:** Draft implementation plan. No code has been implemented or tested as part of creating this document.
**Source:** <mention-page url="https://app.notion.com/p/3fcc64d5199680519a9cda145684cb32">CYBERFORGE : </mention-page>
**Owner:** Akash — gateway foundation. **Backend teammate:** reviews the service contract; their backend is not a prerequisite for this step.
# 1. Outcome and scope
At completion, a teammate can clone the repository, start the gateway with documented environment variables, call its health endpoints, inspect structured logs and stop it cleanly.
This step creates the HTTP service foundation. It does not yet protect or execute an MCP tool call.
**Implement now:** project skeleton, validated configuration, HTTP server, health/readiness endpoints, request correlation, structured logging, panic recovery, graceful shutdown, focused tests and a local runbook.
**Defer:** MCP SDK integration and forwarding, JWT/OAuth verification, agent authorization, policy rules, classifier/Ollama, Redis, PostgreSQL, dashboard, PDF generation and containers. Add these when their respective milestones require them. Do not build empty packages for all future modules.
# 2. Decisions to use
<table header-row="true">
<tr>
<td>Area</td>
<td>Decision</td>
<td>Reason</td>
</tr>
<tr>
<td>HTTP</td>
<td>Go standard library: net/http and ServeMux</td>
<td>Sufficient for the foundation; avoids a framework dependency.</td>
</tr>
<tr>
<td>Logging</td>
<td>log/slog with JSON output to stdout</td>
<td>Machine-readable logs that the backend teammate can later consume.</td>
</tr>
<tr>
<td>Configuration</td>
<td>Environment variables parsed once at startup</td>
<td>Simple local setup and a clear deployment contract.</td>
</tr>
<tr>
<td>Local binding</td>
<td>127.0.0.1:8080 by default</td>
<td>Keep the unauthenticated scaffold local. Broader binding must be deliberate.</td>
</tr>
<tr>
<td>Go version</td>
<td>Agree on a supported stable toolchain compatible with the development/deployment machines; record it in go.mod and README</td>
<td>No repository or local toolchain has been inspected. Confirm compatibility rather than inventing an installed version.</td>
</tr>
<tr>
<td>Dependencies</td>
<td>Standard library only in Step 1</td>
<td>The official MCP SDK is introduced in Step 2 and may raise the minimum Go version.</td>
</tr>
</table>
These are proposed implementation choices. They refine the project architecture without changing its overall Go gateway direction.
# 3. Repository layout
Assume a dedicated gateway repository for these paths. In an existing monorepo, place the same layout under the agreed gateway directory.
<table header-row="true">
<tr>
<td>Path</td>
<td>Responsibility</td>
</tr>
<tr>
<td>cmd/gateway/main.go</td>
<td>Process entry point, configuration loading, dependency wiring and exit status.</td>
</tr>
<tr>
<td>internal/config/config.go</td>
<td>Configuration structure, defaults, parsing and validation.</td>
</tr>
<tr>
<td>internal/config/config_test.go</td>
<td>Tests for defaults, overrides and invalid values.</td>
</tr>
<tr>
<td>internal/gateway/server.go</td>
<td>Construct the HTTP server, bind the listener and coordinate shutdown.</td>
</tr>
<tr>
<td>internal/gateway/routes.go</td>
<td>Route registration and JSON responses.</td>
</tr>
<tr>
<td>internal/gateway/middleware.go</td>
<td>Request ID, access logs and panic recovery.</td>
</tr>
<tr>
<td>internal/gateway/server_test.go</td>
<td>Endpoint, middleware and lifecycle tests.</td>
</tr>
<tr>
<td>.env.example</td>
<td>Documented, non-secret sample settings. Not automatically loaded by the application.</td>
</tr>
<tr>
<td>.gitignore</td>
<td>Ignore .env, local overrides, build output and logs; keep .env.example tracked.</td>
</tr>
<tr>
<td>go.mod</td>
<td>Module identity and Go version. Commit go.sum if dependencies later generate one.</td>
</tr>
<tr>
<td>[README.md](http://README.md)</td>
<td>Setup, configuration, run commands, checks, limitations and teammate handoff.</td>
</tr>
</table>
# 4. Ordered implementation tasks
## Task 1 — Initialize the module and runnable entry point
- [ ] Confirm whether this is a dedicated repository or an existing monorepo.
- [ ] Run go version and record the chosen toolchain in README.
- [ ] Initialize the module using the team's actual repository/module path.
- [ ] Create the directories and files above as they are needed.
- [ ] Keep main.go focused on wiring; put behavior in internal packages.
- [ ] Add the build-output and local-environment exclusions.
**Complete when:** a minimal gateway binary builds successfully from a clean checkout.
Suggested bootstrap commands:
```bash
go version
go mod init <actual-repository-module-path>
mkdir -p cmd/gateway internal/config internal/gateway
```
Replace the module-path placeholder before running. Skip module initialization if the repository already has the correct go.mod.
## Task 2 — Load and validate configuration
Use these proposed local defaults:
<table header-row="true">
<tr>
<td>Variable</td>
<td>Default</td>
<td>Validation / behavior</td>
</tr>
<tr>
<td>GATEWAY_HOST</td>
<td>127.0.0.1</td>
<td>Accept a valid IP address for this milestone; reject empty or malformed values. Broader binding is explicit.</td>
</tr>
<tr>
<td>GATEWAY_PORT</td>
<td>8080</td>
<td>Integer from 1 to 65535.</td>
</tr>
<tr>
<td>LOG_LEVEL</td>
<td>info</td>
<td>One of debug, info, warn or error.</td>
</tr>
<tr>
<td>READ_HEADER_TIMEOUT</td>
<td>5s</td>
<td>Positive Go duration.</td>
</tr>
<tr>
<td>READ_TIMEOUT</td>
<td>10s</td>
<td>Positive Go duration; local non-streaming scaffold only.</td>
</tr>
<tr>
<td>WRITE_TIMEOUT</td>
<td>15s</td>
<td>Positive Go duration; revisit before introducing MCP streaming.</td>
</tr>
<tr>
<td>IDLE_TIMEOUT</td>
<td>60s</td>
<td>Positive Go duration.</td>
</tr>
<tr>
<td>SHUTDOWN_TIMEOUT</td>
<td>10s</td>
<td>Positive Go duration bounding shutdown.</td>
</tr>
</table>
- [ ] Distinguish an unset value from an explicitly empty value; reject empty supplied settings.
- [ ] Parse durations with time.ParseDuration and validate numeric bounds.
- [ ] Combine host and port with net.JoinHostPort.
- [ ] Fail startup with a non-zero exit status and a field-specific error for invalid configuration.
- [ ] Load configuration once; do not read environment variables inside every handler.
- [ ] Do not log the complete environment or introduce unused upstream credentials.
- [ ] Document that .env.example is a reference; no dotenv loader is included.
**Complete when:** defaults work, valid overrides work, and malformed configuration fails before a listener is opened.
## Task 3 — Start an explicit HTTP server
- [ ] Construct an http.Server with the validated address and configured timeouts.
- [ ] Use an explicit ServeMux rather than the global default mux.
- [ ] Open the listener explicitly so bind errors are detected before logging successful startup.
- [ ] Log a listening event only after the port is successfully bound.
- [ ] Treat unexpected serve errors as fatal; distinguish them from http.ErrServerClosed during shutdown.
- [ ] Keep the listener/server references available to lifecycle code.
**Complete when:** the service binds to the expected address and an occupied port produces a clear error and non-zero exit.
The timeout values above are engineering starting points for short health responses, not MCP protocol requirements. Reassess them when Step 2 adds streaming; do not apply short whole-response deadlines blindly to long-lived streams. Reference: [Go HTTP server](https://pkg.go.dev/net/http#Server).
## Task 4 — Implement endpoint contracts
<table header-row="true">
<tr>
<td>Endpoint</td>
<td>Behavior</td>
<td>Purpose</td>
</tr>
<tr>
<td>GET /healthz</td>
<td>200, application/json, body: \{"status":"ok"\}</td>
<td>Liveness: the process can serve a request.</td>
</tr>
<tr>
<td>GET /readyz</td>
<td>200 with \{"status":"ready"\} after initialization; 503 with \{"status":"not_ready"\} when draining if a request reaches the handler</td>
<td>Readiness for the components actually implemented in Step 1.</td>
</tr>
<tr>
<td>/mcp</td>
<td>501 with a JSON not-implemented error; no forwarding or execution</td>
<td>Explicitly reserve the future endpoint without pretending MCP works.</td>
</tr>
<tr>
<td>Unknown paths</td>
<td>404 with a generic JSON error</td>
<td>Predictable scaffold behavior.</td>
</tr>
<tr>
<td>Unsupported health-route methods</td>
<td>405 with the appropriate Allow header</td>
<td>Keep the endpoint contract clear.</td>
</tr>
</table>
- [ ] Do not return environment variables, secrets, internal addresses or stack traces from health endpoints.
- [ ] Do not make readiness depend on FastAPI, Redis, PostgreSQL or Ollama in this milestone.
- [ ] Keep health payloads small and stable.
- [ ] If using GET method patterns that automatically support HEAD, document that behavior.
- [ ] Make clear that Step 1 readiness does not imply MCP forwarding or security protection is operational.
**Complete when:** endpoint tests confirm status codes, content types, payloads and method behavior.
## Task 5 — Add structured logging and request IDs
- [ ] Configure one slog JSON logger at startup using LOG_LEVEL.
- [ ] Generate a gateway-owned request ID using crypto/rand; for example, 16 random bytes encoded as 32 hexadecimal characters.
- [ ] Store the ID in request context and return it in X-Request-ID.
- [ ] Generate a new ID rather than trusting arbitrary incoming X-Request-ID values in this milestone.
- [ ] Emit a request-completed event with request_id, method, route pattern or a bounded route label, status, duration_ms and service name.
- [ ] Log lifecycle events: starting, listening, shutdown_started, shutdown_completed and fatal startup/serve errors.
- [ ] Exclude Authorization, Cookie, raw bodies and URL query strings from access logs.
- [ ] Capture the response status correctly, including implicit 200 and recovered 500 responses.
**Complete when:** a request's response header can be matched to its JSON access log, and supplied dummy credentials/query secrets do not appear in logs.
Reference: [Go structured logging](https://pkg.go.dev/log/slog). Prefer small middleware compatible with the standard library; response wrappers must preserve capabilities needed by future streaming or be replaced before MCP is introduced.
## Task 6 — Add panic recovery
- [ ] Put request ID creation outside the recovery/access-log layers so failures remain correlated.
- [ ] Recover handler panics and log a sanitized internal diagnostic with the request ID.
- [ ] If no response has been committed, return a generic HTTP 500 JSON error.
- [ ] Do not expose panic values or stack traces to callers.
- [ ] Do not attempt a second JSON response after headers/body have already been committed; log the failure instead.
- [ ] Use a test-only panic handler; do not expose a public panic route.
**Complete when:** a panic before response writing returns a correlated 500, the process stays alive, and a subsequent health request succeeds.
## Task 7 — Implement graceful shutdown
- [ ] Use signal.NotifyContext for SIGINT and SIGTERM.
- [ ] On shutdown, mark readiness false and log shutdown_started.
- [ ] Call Server.Shutdown with a fresh context and SHUTDOWN_TIMEOUT; do not reuse the already-canceled signal context.
- [ ] Allow existing short requests to finish during the grace period. Do not cancel all request contexts immediately on the first signal.
- [ ] Keep the process alive until Shutdown returns; Serve returning ErrServerClosed is not by itself proof that draining has completed.
- [ ] If the shutdown deadline expires, log it and force-close remaining connections.
- [ ] Exit zero after a clean shutdown; exit non-zero for startup failure, unexpected serve failure or forced shutdown failure.
**Complete when:** the built binary handles SIGINT and SIGTERM predictably, a controlled in-flight request completes within grace, and a deliberately stuck request cannot hold the process open indefinitely.
References: [NotifyContext](https://pkg.go.dev/os/signal#NotifyContext) and [Server.Shutdown](https://pkg.go.dev/net/http#Server.Shutdown). Shutdown does not manage all upgraded/hijacked connections; revisit lifecycle handling when later transports introduce them.
## Task 8 — Write the runbook and hand off the foundation
- [ ] Document prerequisites, the chosen Go version and the directory from which commands should run.
- [ ] Document every environment variable and the fact that .env is not automatically read.
- [ ] Include expected health responses and how to change a conflicting port.
- [ ] Document what Step 1 implements and that /mcp is intentionally unavailable.
- [ ] Share endpoint contracts and proposed log fields with the backend teammate.
- [ ] Record unresolved deployment choices; no database or backend integration is required for acceptance.
# 5. Validation plan
Use focused tests for failure behavior and lifecycle guarantees, rather than tests that merely mirror every helper.
<table header-row="true">
<tr>
<td>Scenario</td>
<td>Expected result</td>
</tr>
<tr>
<td>No environment overrides</td>
<td>Configuration resolves to the documented defaults.</td>
</tr>
<tr>
<td>Valid port, address and duration overrides</td>
<td>Overrides are used by the server.</td>
</tr>
<tr>
<td>Empty/invalid port, unknown log level or non-positive duration</td>
<td>Startup fails before binding with the setting named in the error.</td>
</tr>
<tr>
<td>Health and readiness requests</td>
<td>Exact documented status, content type and JSON payload.</td>
</tr>
<tr>
<td>Unknown path, unsupported method and /mcp</td>
<td>Documented 404, 405 and 501 behavior.</td>
</tr>
<tr>
<td>Dummy Authorization/Cookie headers and a secret query parameter</td>
<td>Those values are absent from captured access logs.</td>
</tr>
<tr>
<td>Recovered panic before response commit</td>
<td>Generic 500, request ID and a healthy subsequent request.</td>
</tr>
<tr>
<td>Concurrent requests</td>
<td>Distinct correlation IDs and no readiness/logging data races.</td>
</tr>
<tr>
<td>Port already occupied</td>
<td>Non-zero exit; no false listening-success log.</td>
</tr>
<tr>
<td>In-flight request during shutdown</td>
<td>Completes within grace; readiness flips false before draining.</td>
</tr>
<tr>
<td>Shutdown deadline exceeded</td>
<td>Bounded exit with a clear failure diagnostic.</td>
</tr>
</table>
Use net/http/httptest for handler tests. Use a listener on an OS-assigned port in lifecycle tests to avoid collisions. Verify process signals against the built binary because go run involves a wrapper process.
# 6. Local verification commands
Run from the gateway module root after implementation:
```bash
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go build -o ./bin/gateway ./cmd/gateway
```
The race detector depends on platform/toolchain support and may need a C compiler. If unavailable locally, run that check on a supported CI/development machine and record the result.
Start in terminal A:
```bash
GATEWAY_HOST=127.0.0.1 GATEWAY_PORT=8080 LOG_LEVEL=debug ./bin/gateway
```
Check in terminal B:
```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
curl -i http://127.0.0.1:8080/mcp
curl -i http://127.0.0.1:8080/does-not-exist
curl -i -X POST http://127.0.0.1:8080/healthz
```
Expected status sequence: 200, 200, 501, 404, 405. Confirm an X-Request-ID header and matching structured log for each request. Press Ctrl+C in terminal A and inspect shutdown logs. Test SIGTERM using the PID of this exact built process; avoid broad process-kill commands.
# 7. Definition of done
- [ ] Clean checkout builds using the documented toolchain.
- [ ] The binary starts with defaults and valid environment overrides.
- [ ] Invalid configuration and occupied ports produce non-zero exits.
- [ ] Health, readiness, error and reserved /mcp contracts pass.
- [ ] Structured logs contain request correlation without dummy credentials or query secrets.
- [ ] Recovery keeps the service alive after a test panic.
- [ ] Graceful shutdown drains short requests and remains time-bounded.
- [ ] Formatting, vet, tests, race check on a supported machine and build pass.
- [ ] README enables the backend teammate to run and verify the service independently.
- [ ] No authentication, forwarding or security-enforcement capability is claimed yet.
# 8. Handoff to Step 2
Once the checklist passes, add the official Go MCP SDK, choose and document transport/protocol compatibility, replace the /mcp placeholder, and connect one controlled upstream server. Revisit response-wrapper capabilities, streaming deadlines and connection cleanup before introducing long-lived responses.
Step 1 produces an HTTP foundation. Step 2 proves MCP connectivity. Later steps add the identity and policy enforcement required for the gateway to protect tool execution.
# References
- Project requirements: <mention-page url="https://app.notion.com/p/3fcc64d5199680519a9cda145684cb32">CYBERFORGE : </mention-page>
- [Go dependency management](https://go.dev/doc/modules/managing-dependencies) — module and dependency workflow.
- [Go HTTP server](https://pkg.go.dev/net/http#Server) — server configuration and lifecycle APIs.
- [Go structured logging](https://pkg.go.dev/log/slog) — JSON logging and level control.
- [Go process signals](https://pkg.go.dev/os/signal#NotifyContext) — signal-triggered cancellation.
- [Official Go MCP SDK](https://github.com/modelcontextprotocol/go-sdk) — follow-on integration in Step 2.
