# Step 2 — MCP Connection and Tool Forwarding

Source: https://app.notion.com/p/3fcc64d519968184bd7cc88cc04aefd8?pvs=204

Local source snapshot retrieved 2026-10-17. Detailed planning remains in Notion; implementation evidence is recorded in README.md.

**Purpose:** Extend the completed HTTP foundation into a working local MCP tool gateway: MCP test client → CYBERFORGE → controlled upstream MCP server → result.
**Status:** Implementation plan only. No Step 2 code or tests have been executed while preparing this page.
**Owner:** Akash — Go gateway and MCP integration. The backend teammate’s FastAPI, storage and dashboard work is not a prerequisite.
**Project:** <mention-page url="https://app.notion.com/p/3fcc64d5199680519a9cda145684cb32">CYBERFORGE : </mention-page>
**Prerequisite:** <mention-page url="https://app.notion.com/p/3fcc64d5199681df9f08c9bce2b47281"/>
# 1. Starting point and completion target
Step 1 is reported complete by the implementation agent: configuration validation, local HTTP listener, health/readiness, request IDs, JSON access logs, recovery, timeouts and bounded graceful shutdown. Tests, race detection, vet, build and manual binary checks reportedly passed. This is user-provided implementation evidence; the repository was not inspected for this plan.
Inspect the actual repository before editing and preserve its working structure. The Step 1 Notion page still describes the original plan; do not assume its draft status means the code is missing.
**At completion:** a clean checkout can start a harmless upstream fixture and the gateway, then run an MCP client that discovers a proxied tool and calls it successfully. Failure cases produce bounded, protocol-appropriate errors, and shutdown releases MCP resources.
**This proves connectivity and forwarding. It does not establish authenticated or policy-controlled execution.**
# 2. Scope and implementation decisions
<table header-row="true">
<tr>
<td>Area</td>
<td>Step 2 decision</td>
</tr>
<tr>
<td>SDK</td>
<td>Use [github.com/modelcontextprotocol/go-sdk/mcp](http://github.com/modelcontextprotocol/go-sdk/mcp). Select an available supported stable release, check its Go requirements and pin an exact version in go.mod/go.sum.</td>
</tr>
<tr>
<td>Transport</td>
<td>Streamable HTTP on both links. Gateway: [http://127.0.0.1:8080/mcp](http://127.0.0.1:8080/mcp). Fixture: [http://127.0.0.1:9090/mcp](http://127.0.0.1:9090/mcp).</td>
</tr>
<tr>
<td>Protocol target</td>
<td>Target 2026-07-28 with a compatible stable SDK. Record the selected SDK, Go version and tested protocol in the README. Do not assume older handshake/session examples describe this revision.</td>
</tr>
<tr>
<td>Topology</td>
<td>Exactly one configured upstream and one local demonstration client. No multi-tenant session pool or server registry.</td>
</tr>
<tr>
<td>Tool catalog</td>
<td>Discover at startup and keep an immutable routing snapshot. Restart to refresh the catalog in this milestone.</td>
</tr>
<tr>
<td>Execution</td>
<td>One forwarded invocation per accepted tool call. No automatic retries of tools/call.</td>
</tr>
<tr>
<td>Demo boundary</td>
<td>Bind both processes to loopback and use synthetic, harmless tools. Reject non-loopback deployment in this milestone until authentication is implemented.</td>
</tr>
<tr>
<td>Backend dependencies</td>
<td>None. Keep existing JSON logging; do not add Redis, PostgreSQL or FastAPI calls.</td>
</tr>
</table>
These are project choices, not universal MCP requirements. If the repository or installed toolchain cannot support the target SDK, document the concrete compatibility blocker before changing the protocol baseline; do not silently mix revisions.
**Implement now:** SDK wiring, MCP endpoint, upstream connection, tool discovery, stable tool routing, argument/result forwarding, bounded failures, transport-compatible middleware, lifecycle cleanup, integration tests and a reproducible runbook.
**Out of scope:** JWT/OAuth implementation, agent roles, authorization policies, path/destination security rules, tool-definition approval/hashing, injection detection, AI/Ollama, durable audit events, dashboard, reports, containers, public deployment, multi-upstream aggregation, hot reload, subscription forwarding, sampling, elicitation, prompts and resource APIs. Do not create empty modules for these.
# 3. Protocol compatibility guardrails
Official SDK documentation currently lists support for protocol 2026-07-28 in the v1.7.0-and-later series. Verify the exact released tag and matching API documentation when implementing; do not copy main-branch APIs into an older pinned release. [SDK compatibility reference](https://github.com/modelcontextprotocol/go-sdk#version-compatibility).
The 2026-07-28 transport revision uses request metadata rather than the earlier connection-scoped initialize/session model. POST replies can use JSON or request-scoped SSE; version-specific headers and cancellation behavior matter. GET streams and protocol session IDs from older examples are not requirements of this target. Use the SDK for the selected revision and test actual wire behavior. [Transport reference](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http).
The gateway terminates one MCP connection and originates another. Do not implement a blind HTTP reverse proxy or hand-written JSON-RPC parser. Do not invent REST routes such as POST /tools/call: tools/list and tools/call are protocol methods carried through /mcp.
# 4. Repository changes
Adapt these paths to the existing repository. Keep the module name and established naming conventions.
<table header-row="true">
<tr>
<td>Path</td>
<td>Responsibility</td>
</tr>
<tr>
<td>internal/mcpbridge/upstream.go</td>
<td>SDK client, upstream transport, discovery and resource cleanup.</td>
</tr>
<tr>
<td>internal/mcpbridge/catalog.go</td>
<td>Immutable gateway-name to upstream identity/tool mapping.</td>
</tr>
<tr>
<td>internal/mcpbridge/bridge.go</td>
<td>Gateway MCP server, exposed tool definitions and forwarding handlers.</td>
</tr>
<tr>
<td>internal/mcpbridge/\*_test.go</td>
<td>Routing, result preservation and controlled failure tests.</td>
</tr>
<tr>
<td>internal/gateway/ existing files</td>
<td>Mount /mcp, preserve streaming capabilities, readiness and shutdown wiring.</td>
</tr>
<tr>
<td>internal/config/ existing files</td>
<td>Additional validated settings and tests.</td>
</tr>
<tr>
<td>cmd/demo-upstream/main.go</td>
<td>Local harmless MCP fixture; no filesystem, shell, database or real credentials.</td>
</tr>
<tr>
<td>cmd/demo-client/main.go</td>
<td>SDK smoke client that lists tools, calls demo.echo and verifies its result.</td>
</tr>
<tr>
<td>tests/integration/ or existing equivalent</td>
<td>Real loopback HTTP tests covering both links.</td>
</tr>
<tr>
<td>[README.md](http://README.md) and .env.example</td>
<td>Updated configuration, startup, verification and limitations.</td>
</tr>
<tr>
<td>go.mod and go.sum</td>
<td>Pinned SDK and resolved dependency checksums.</td>
</tr>
</table>
# 5. Configuration contract
Retain Step 1 settings unless a documented change is necessary. The following additions/defaults are proposed engineering values for this local demo.
<table header-row="true">
<tr>
<td>Setting</td>
<td>Default</td>
<td>Validation / meaning</td>
</tr>
<tr>
<td>MCP_UPSTREAM_ID</td>
<td>demo</td>
<td>Stable configured ID; short ASCII letters, digits, hyphen or underscore. Do not derive identity from the upstream display name.</td>
</tr>
<tr>
<td>MCP_UPSTREAM_URL</td>
<td>[http://127.0.0.1:9090/mcp](http://127.0.0.1:9090/mcp)</td>
<td>Absolute URL with explicit loopback IP and port. Reject userinfo, query, fragment, redirects and non-loopback destinations for this demo.</td>
</tr>
<tr>
<td>MCP_CONNECT_TIMEOUT</td>
<td>5s</td>
<td>Positive duration bounding initial connection/probe work.</td>
</tr>
<tr>
<td>MCP_DISCOVERY_TIMEOUT</td>
<td>5s</td>
<td>Positive duration bounding the entire startup catalog load.</td>
</tr>
<tr>
<td>MCP_CALL_TIMEOUT</td>
<td>5s</td>
<td>Positive duration for one forwarded tool invocation.</td>
</tr>
<tr>
<td>MCP_MAX_REQUEST_BYTES</td>
<td>1048576</td>
<td>Positive byte cap applied before MCP body parsing.</td>
</tr>
<tr>
<td>SHUTDOWN_TIMEOUT</td>
<td>Retain Step 1 default</td>
<td>One shared overall drain/cleanup budget; avoid stacking independent full-length shutdown waits.</td>
</tr>
</table>
No upstream token or secret is required. Do not forward incoming Authorization, Cookie or arbitrary transport headers to the upstream. The SDK should construct its own protocol requests; gateway-side identifiers/headers may need regeneration after tool-name translation.
Preserve the Step 1 unset-versus-empty validation rule. Document that .env.example is a reference and is not automatically loaded.
# 6. Ordered implementation tasks
## Task 1 — Inspect the foundation and pin the SDK
- [ ] Read repository instructions, existing configuration, routing, middleware and lifecycle tests.
- [ ] Run the existing checks once to establish the baseline.
- [ ] Check go version and the chosen SDK release’s go.mod/API documentation.
- [ ] Add an exact SDK release; commit go.mod/go.sum and record compatibility.
- [ ] Use exported SDK facilities for HTTP handling, metadata, tool registration/calls and client cleanup.
- [ ] Keep main focused on composition.
**Complete when:** the existing gateway still builds and its tests pass with the pinned SDK. Record any changed Go requirement; do not silently upgrade the developer’s machine.
## Task 2 — Build the controlled upstream fixture
- [ ] Serve MCP at 127.0.0.1:9090/mcp using the same pinned SDK and transport.
- [ ] Expose echo with one required string argument, message.
- [ ] Return message in a text content block; optionally include matching structured content with a matching output schema.
- [ ] Include only harmless test fixtures for delay and a deliberate tool error, either in the fixture process or test-only setup.
- [ ] Make delay honor context cancellation. Record invocation count and cancellation observations inside tests.
- [ ] Keep fixture logging separate from protocol responses.
Suggested echo input schema:
```json
{
  "type": "object",
  "properties": {
    "message": {"type": "string"}
  },
  "required": ["message"],
  "additionalProperties": false
}
```
**Complete when:** an SDK test client can discover and call echo directly. This isolates fixture problems before adding the gateway.
## Task 3 — Connect the gateway and load its catalog
- [ ] Construct the SDK client/HTTP transport from validated configuration.
- [ ] Establish the client relationship/probe according to the pinned SDK’s protocol behavior, within the connection budget.
- [ ] Retrieve tools/list through the SDK; consume pagination if present.
- [ ] Bound the full discovery operation and reject an empty, unusable or ambiguous catalog for this fixture.
- [ ] Copy definitions into a routing snapshot; do not mutate shared SDK objects concurrently.
- [ ] Retain only capabilities the gateway actually implements.
- [ ] Set readiness only after upstream discovery and bridge wiring succeed.
- [ ] On startup connection/discovery failure, close partial resources and exit non-zero with a sanitized reason.
**Complete when:** startup with the fixture succeeds; startup without it fails within the documented bound without reporting ready.
## Task 4 — Mount the MCP server and expose discovered tools
- [ ] Replace the Step 1 /mcp 501 placeholder with the SDK Streamable HTTP handler.
- [ ] Keep /healthz and /readyz separate from protocol routes.
- [ ] Name the exposed tool demo.echo and maintain an explicit mapping to upstream ID demo and original name echo.
- [ ] Reject duplicate/invalid mapped names; do not decode arbitrary caller-provided prefixes into destinations.
- [ ] Preserve supported definition fields: description, input schema, output schema and annotations where present.
- [ ] Validate/handle any protocol-specific schema extensions using the pinned SDK rather than silently discarding required behavior.
- [ ] Advertise tool support only. Do not claim prompts, resources or subscriptions are proxied.
- [ ] Document that catalog updates require restart; this snapshot is not a security approval mechanism.
**Complete when:** a client connected to the gateway lists demo.echo with the expected schema.
## Task 5 — Forward calls and preserve results
- [ ] Look up the requested tool in the immutable routing map before upstream invocation.
- [ ] Translate demo.echo to echo and send arguments without lossy string conversion or float-based re-parsing.
- [ ] Use the inbound request context and a per-call timeout for the upstream operation.
- [ ] Preserve supported result content blocks, structured content and the tool-error indicator for the selected protocol.
- [ ] Preserve application metadata only where its meaning survives the bridge; do not blindly copy SDK-managed protocol metadata.
- [ ] Return an unknown-tool/protocol error without contacting the upstream.
- [ ] Return invalid arguments according to SDK/schema handling; malformed input must not execute the fixture.
- [ ] Translate network/timeout failures into a stable sanitized MCP failure. Do not put a generic HTTP JSON envelope around an otherwise valid MCP result.
- [ ] Never automatically retry a call after a timeout or broken connection: execution outcome may be uncertain.
- [ ] Never execute shell commands, local file operations or substitute tools as a fallback.
**Complete when:** a message round-trips through both HTTP links unchanged, and failed calls do not appear successful.
## Task 6 — Make HTTP middleware compatible with MCP
- [ ] Test the Step 1 response recorder with streaming. Preserve flushing and ResponseController unwrapping capabilities required by the SDK.
- [ ] Ensure recovery/access logging cannot append generic JSON after an SSE or protocol response is committed.
- [ ] Apply a request-body cap before decoding and verify oversized input never invokes the upstream.
- [ ] Retain read-header protection; review whole-request read/write deadlines against MCP responses.
- [ ] Do not reuse a short global response deadline or http.TimeoutHandler that truncates legitimate SSE responses. If changing a server-wide timeout, document the replacement bounds for /mcp and health routes.
- [ ] Validate present Origin values against an explicit local allowlist and return 403 for unapproved origins before invocation. Allow an absent Origin for the native demo client. Do not enable wildcard CORS.
- [ ] Let the SDK enforce version-specific method/header/metadata rules. Add a focused test for malformed/mismatched metadata where applicable.
- [ ] Confirm JSON responses and request-scoped SSE both work with the pinned transport.
**Complete when:** protocol framing remains valid through middleware and a controlled streaming request is not buffered or cut off by the old scaffold.
## Task 7 — Define readiness, failure and shutdown behavior
- [ ] /healthz continues to describe process responsiveness.
- [ ] /readyz is false during startup/drain and after an observed fatal upstream connection failure. It is not proof of authorization or policy protection.
- [ ] Do not perform a fresh upstream request for every readiness probe.
- [ ] On a fatal upstream failure, reject new forwarded calls predictably. For this milestone, recover by restarting the gateway; automatic reconnection and catalog refresh are deferred.
- [ ] On shutdown, mark not ready, stop admitting new work and let in-flight calls finish within the overall grace budget.
- [ ] Keep upstream resources alive while permitted calls drain.
- [ ] At deadline, cancel remaining work and close SDK-owned connections/streams and HTTP resources using supported APIs.
- [ ] Distinguish the SDK’s client/session object lifecycle from protocol session semantics. Do not invent protocol session IDs for the target revision.
- [ ] Close response bodies/transports and avoid leaking goroutines or idle connections.
**Complete when:** SIGINT/SIGTERM drain a short call, bound a stuck call and leave no owned client/stream alive after exit.
## Task 8 — Extend logs and write the runbook
- [ ] Keep gateway-generated X-Request-ID and correlate it with MCP forwarding logs.
- [ ] Add bounded fields such as upstream_id, exposed tool, original tool, outcome, reason_code and duration_ms.
- [ ] Distinguish HTTP success from tool success: an HTTP 200 may still carry a tool failure.
- [ ] Use outcomes such as succeeded, tool_error, invalid_request, upstream_unavailable, deadline_exceeded and cancelled.
- [ ] Do not call an anonymous local client a verified agent or claim a policy allow decision.
- [ ] Continue excluding credentials, cookies, raw arguments/results, query strings and session identifiers.
- [ ] Provide executable demo-client commands and expected output; a plain curl GET /mcp is not proof that MCP works.
- [ ] State that the unauthenticated local demo must not be exposed to real agents with sensitive tools.
**Complete when:** the teammate can reproduce the full flow using README alone without the management backend.
# 7. Verification matrix
Use real loopback HTTP listeners on OS-assigned ports for integration tests, plus fixture counters/observations. Mocks alone cannot verify framing and streaming.
<table header-row="true">
<tr>
<td>Scenario</td>
<td>Acceptance evidence</td>
</tr>
<tr>
<td>Discovery</td>
<td>Gateway client lists demo.echo with the fixture schema; pagination covered if discovery code handles multiple pages.</td>
</tr>
<tr>
<td>Successful echo</td>
<td>Message returned unchanged; exactly one upstream invocation; request/log correlation available.</td>
</tr>
<tr>
<td>Structured result and tool error</td>
<td>Supported result fields and tool-error status survive the bridge.</td>
</tr>
<tr>
<td>Unknown tool</td>
<td>Protocol-appropriate failure and zero upstream invocations.</td>
</tr>
<tr>
<td>Invalid/malformed arguments</td>
<td>Clear failure; fixture invocation count does not increase.</td>
</tr>
<tr>
<td>Invalid Origin and oversized body</td>
<td>Transport rejection before upstream execution.</td>
</tr>
<tr>
<td>Unsupported version or inconsistent metadata</td>
<td>Pinned SDK’s documented protocol/HTTP failure; no tool execution.</td>
</tr>
<tr>
<td>Startup without upstream</td>
<td>Bounded non-zero startup failure, no false readiness.</td>
</tr>
<tr>
<td>Upstream disconnects after startup</td>
<td>Bounded sanitized call error; fatal connection loss makes readiness false; no retry/replay.</td>
</tr>
<tr>
<td>Tool exceeds deadline</td>
<td>Caller receives a bounded failure and fixture observes cancellation where supported; no second invocation.</td>
</tr>
<tr>
<td>Client cancels</td>
<td>Cancellation propagates using the selected transport; fixture stops cooperative work; unrelated requests remain usable.</td>
</tr>
<tr>
<td>JSON and SSE responses</td>
<td>SDK client parses both successfully through the existing middleware.</td>
</tr>
<tr>
<td>Concurrent calls</td>
<td>Correct response-to-call association and no catalog/logging races.</td>
</tr>
<tr>
<td>Drain and forced shutdown</td>
<td>Short in-flight work completes; stuck work cannot extend the shutdown budget indefinitely.</td>
</tr>
<tr>
<td>Log redaction</td>
<td>Dummy secrets in arguments, results and headers are absent from logs.</td>
</tr>
<tr>
<td>Foundation regression</td>
<td>Existing health, configuration, recovery and signal tests pass; update only intentional /mcp and readiness expectations.</td>
</tr>
</table>
Test protocol lifecycle/cancellation against the selected version rather than asserting older initialization or session behavior. Claim compatibility only for combinations actually tested.
# 8. Local runbook to implement
The following paths and flags are the proposed interface for the new demo binaries. Implement and document them, or update this section consistently with repository conventions.
Run from the gateway module root:
```bash
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go build -o ./bin/gateway ./cmd/gateway
go build -o ./bin/demo-upstream ./cmd/demo-upstream
go build -o ./bin/demo-client ./cmd/demo-client
```
Terminal A — harmless fixture:
```bash
./bin/demo-upstream -addr 127.0.0.1:9090
```
Terminal B — gateway:
```bash
GATEWAY_HOST=127.0.0.1 GATEWAY_PORT=8080 MCP_UPSTREAM_ID=demo MCP_UPSTREAM_URL=http://127.0.0.1:9090/mcp ./bin/gateway
```
Terminal C — health and real MCP smoke client:
```bash
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
./bin/demo-client -url http://127.0.0.1:8080/mcp -message "CYBERFORGE Step 2"
```
The demo client must discover demo.echo, invoke it with the supplied message, verify the returned text and exit zero only on success. Unexpected result, tool error, network failure or deadline must exit non-zero. It connects only to the gateway during this smoke test.
Stop the gateway with Ctrl+C and verify drain/cleanup logs. Exercise SIGTERM against the exact built process in automated lifecycle tests. Stop the fixture independently; the gateway must not terminate an externally owned upstream process.
If race detection is unavailable on the local machine, run it on a supported development/CI platform and record that limitation honestly.
# 9. Definition of done
- [ ] Exact SDK, Go and tested protocol versions documented and reproducible.
- [ ] One controlled upstream works over Streamable HTTP.
- [ ] /mcp uses the SDK and replaces the 501 placeholder.
- [ ] Discovery exposes demo.echo with stable upstream identity mapping.
- [ ] Successful call returns an unchanged message across both links.
- [ ] Invalid/unknown calls and oversized/rejected transport requests do not invoke the upstream.
- [ ] Results, tool errors, upstream failures and timeout outcomes are distinguishable.
- [ ] Cancellation, JSON/SSE behavior, middleware and shutdown pass integration checks.
- [ ] No automatic tool-call retries or unintended fallback execution.
- [ ] Existing Step 1 checks still pass with intentional contract changes documented.
- [ ] README and .env.example match the implementation.
- [ ] Format, vet, tests, supported race check and builds pass.
- [ ] Evidence includes commands/results or test output, not only an implementation summary.
- [ ] Scope remains a local connectivity milestone; no security-enforcement claim.
# 10. Copy-ready handoff for Codex CLI
```plain text
Implement only CYBERFORGE Step 2 using this plan and the existing Step 1 repository.

First inspect AGENTS.md, the repository structure, current configuration, HTTP middleware, lifecycle code and tests. Preserve the existing working foundation. Check the official Go MCP SDK release documentation and pin an exact supported stable dependency compatible with the documented target protocol.

Build the gateway as an MCP server toward the local demo client and an MCP client toward one harmless local upstream, using Streamable HTTP. Implement startup discovery, stable demo.echo routing, argument/result forwarding, bounded errors, cooperative cancellation, stream-compatible middleware, readiness and cleanup. Add the fixture, SDK smoke client, focused integration tests and a reproducible README.

Use the ordered tasks, configuration contract, verification matrix and definition of done in this document as acceptance criteria. Do not implement later identity, policy, classifier or management-backend features. Do not expose the unauthenticated demo outside loopback or connect sensitive tools.

Resolve routine implementation choices within this plan. If the actual SDK/toolchain cannot support the selected protocol, report the concrete blocker before changing the baseline. Run the applicable checks and report changed behavior, evidence, limitations and anything unfinished. Do not mark unrun checks as passed.
```
Provide the full page content or a repository copy to CLI if it cannot access Notion directly.
# 11. Documentation preference for this project
**Use English. Keep detailed implementation plans in Notion. Create or expand each future step only when Akash explicitly asks to move to that step. This page covers Step 2 only; no future-step plans are being created now.**
# References
- <mention-page url="https://app.notion.com/p/3fcc64d5199680519a9cda145684cb32">CYBERFORGE : </mention-page>
- <mention-page url="https://app.notion.com/p/3fcc64d5199681df9f08c9bce2b47281"/>
- [Official Go MCP SDK and compatibility matrix](https://github.com/modelcontextprotocol/go-sdk) — select and pin release documentation.
- [Official SDK server feature guide](https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/server.md) — tool registration, schema handling and capabilities; use the chosen release’s equivalent.
- [Official SDK client feature guide](https://github.com/modelcontextprotocol/go-sdk/blob/main/docs/client.md) — client operations; use the chosen release’s equivalent.
- [MCP 2026-07-28 transport overview](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports) — revision-specific transport model.
- [MCP 2026-07-28 Streamable HTTP](https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http) — HTTP framing, metadata, origin validation and cancellation.
- [MCP 2026-07-28 tools](https://modelcontextprotocol.io/specification/2026-07-28/server/tools) — discovery, schemas and results.
Official documentation checked when preparing this plan. All project defaults, file paths, task order and acceptance cases above are proposed implementation decisions; they are not claims that implementation has already occurred.
