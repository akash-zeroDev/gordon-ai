# CYBERFORGE gateway — Step 2

CYBERFORGE now connects a local MCP client to one harmless upstream MCP server:

```text
SDK demo client → POST /mcp on gateway → upstream echo tool → unchanged result
                 demo.echo             echo
```

Step 1 supplied the HTTP service foundation. Step 2 adds real MCP discovery and
forwarding. **This is an unauthenticated local connectivity demo, not a security
enforcement gateway.** JWT/OAuth, authorization, policies, AI inspection and
management/backend integrations remain outside this milestone. Both servers must
stay on loopback; use only synthetic tools and messages.

## Compatibility and prerequisites

Run commands from this directory with **Go 1.26.5 or newer**. The existing project
Go version remains unchanged. The pinned SDK dependency is the official
[`github.com/modelcontextprotocol/go-sdk v1.8.0`](https://github.com/modelcontextprotocol/go-sdk/releases/tag/v1.8.0)
(requires Go 1.25). `go.mod` and `go.sum` pin dependencies.
`github.com/santhosh-tekuri/jsonschema/v6 v6.0.3` validates raw arguments without
losing numeric precision. This additional validator avoids the SDK validator's
`json.Number` type-classification limitation; MCP and schema header extensions
still use the official SDK.

The supported/test target is **MCP 2026-07-28**, using Streamable HTTP on both links.
Servers advertise only this revision and tools; the client checks the negotiated
revision and rejects a downgrade. This revision uses `server/discover` and
per-request metadata, not the older initialization handshake, GET subscription
stream or protocol session ID model. SDK `ClientSession` is a local resource
handle; it does not imply an MCP protocol session.

## Build and run the full demonstration

Build all three binaries:

```sh
go build -o ./bin/gateway ./cmd/gateway
go build -o ./bin/demo-upstream ./cmd/demo-upstream
go build -o ./bin/demo-client ./cmd/demo-client
```

Terminal A — start the harmless upstream:

```sh
./bin/demo-upstream -addr 127.0.0.1:9090
```

Terminal B — start the gateway **after the fixture is listening**:

```sh
GATEWAY_HOST=127.0.0.1 GATEWAY_PORT=8080 \
MCP_UPSTREAM_ID=demo MCP_UPSTREAM_URL=http://127.0.0.1:9090/mcp \
./bin/gateway
```

Terminal C — check health, then use a real MCP client:

```sh
curl -i http://127.0.0.1:8080/healthz
curl -i http://127.0.0.1:8080/readyz
./bin/demo-client -url http://127.0.0.1:8080/mcp -message "CYBERFORGE Step 2"
```

Expected output:

```text
PASS: discovered demo.echo; echo matched; protocol 2026-07-28
```

The smoke client lists tools, finds `demo.echo`, calls it once and verifies the
returned text. It connects only to the gateway. Missing tools, mismatched text,
tool errors, protocol mismatch, connection failures or its 15-second overall
deadline produce a nonzero exit. The message is not printed to the console.

For direct fixture diagnosis, before using the gateway:

```sh
./bin/demo-client -url http://127.0.0.1:9090/mcp -tool echo -message "direct check"
```

The fixture exposes only `echo`, with a required string `message`, no additional
arguments, and matching text/structured output. Delay and error tools exist only
in tests. Start the fixture with `-json` to exercise JSON replies; its default is
request-scoped SSE. The gateway uses request-scoped SSE replies and consumes both
JSON and SSE upstream responses. There is no standalone GET stream.

Press Ctrl+C in each server's terminal to stop it independently. The gateway never
terminates the externally owned upstream process. For SIGTERM use the PID of the
exact built process, for example `kill -TERM "$gateway_pid"` after starting it with
`./bin/gateway & gateway_pid=$!`. Use the binary, not `go run`, for signal checks.

## Configuration

Environment variables are read once and validated before upstream I/O or binding.
Unset variables use defaults; explicitly empty values are rejected. The service
never loads `.env` automatically. `.env.example` contains no secrets. To explicitly
export it in a POSIX-compatible shell:

```sh
set -a
. ./.env.example
set +a
```

| Variable | Default | Meaning / validation |
| --- | --- | --- |
| `GATEWAY_HOST` | `127.0.0.1` | Loopback IP literal only; `::1` is valid, `0.0.0.0` and hostnames are rejected |
| `GATEWAY_PORT` | `8080` | Integer 1–65535 |
| `LOG_LEVEL` | `info` | Exactly `debug`, `info`, `warn`, or `error` |
| `READ_HEADER_TIMEOUT` | `5s` | Positive duration for request headers |
| `READ_TIMEOUT` | `10s` | Positive duration for reading the complete request |
| `WRITE_TIMEOUT` | `15s` | Positive health/error write budget; final-write allowance for MCP |
| `IDLE_TIMEOUT` | `60s` | Positive keep-alive idle timeout |
| `SHUTDOWN_TIMEOUT` | `10s` | One shared HTTP drain + MCP cleanup budget |
| `MCP_UPSTREAM_ID` | `demo` | 1–32 ASCII letters, digits, `_` or `-`; stable routing identity |
| `MCP_UPSTREAM_URL` | `http://127.0.0.1:9090/mcp` | Absolute HTTP URL, explicit loopback IP, port and path; no userinfo, query or fragment |
| `MCP_CONNECT_TIMEOUT` | `5s` | Positive bound for initial SDK connection/discovery probe |
| `MCP_DISCOVERY_TIMEOUT` | `5s` | Positive bound for the entire paginated tool catalog load |
| `MCP_CALL_TIMEOUT` | `5s` | Positive deadline for one upstream invocation |
| `MCP_MAX_REQUEST_BYTES` | `1048576` | Positive raw body limit, enforced before SDK decoding, including chunked requests |

Change `GATEWAY_PORT` if the port is occupied. To move the upstream, change its
`-addr` and `MCP_UPSTREAM_URL` together. Changing the upstream ID changes exposed
names; pass the corresponding `-tool new_id.echo` to the smoke client.

Step 2 deliberately tightens Step 1's binding rule: non-loopback listeners and
upstreams are rejected. Redirects and environment HTTP proxies are disabled.
There are no upstream credentials, cookie jar or forwarded inbound credentials.

## Service and transport behavior

| Endpoint | Behavior |
| --- | --- |
| `GET /healthz` | 200, `application/json`, `{"status":"ok"}` |
| `GET /readyz` | 200, `{"status":"ready"}` only after catalog loading/wiring; 503, `{"status":"not_ready"}` during drain or after observed fatal upstream loss |
| Health/readiness methods other than GET | 405 with `Allow: GET`; HEAD has no response body |
| `/mcp` | SDK Streamable HTTP endpoint; replaces the Step 1 501 placeholder |
| Unknown paths | 404 JSON `{"error":"not_found"}` |

Startup loads every catalog page, rejects empty/duplicate/invalid catalogs and
caps the snapshot at 1,024 tools. A failed connection or catalog load exits
nonzero before the gateway listens. The upper network budget is connect plus
discovery timeouts. Readiness never performs a new upstream probe itself.

Tool names map through the startup snapshot: `demo.echo` maps to configured ID
`demo` and original tool `echo`. Arbitrary caller prefixes cannot select a
connection or destination. Definitions retain descriptions, schemas, titles,
icons and annotations. Supported `x-mcp-header` schema annotations are validated
by the SDK, preserved, and regenerated for the translated upstream request.
Unsupported schemas/references fail startup; remote schema references are not
fetched. Catalog changes require a restart and are not security approvals.

Arguments are validated using JSON numbers without float conversion, then
forwarded as the original raw JSON. No defaults are injected. Content blocks,
structured content and `isError` are preserved as represented by the SDK. Only
application result metadata under `com.cyberforge/` crosses the bridge; other
metadata, input responses and opaque request state are not forwarded. Results
requesting sampling/elicitation or another round trip are rejected. The SDK's
usual dynamic structured-content decoding applies; exact arbitrary-precision
numeric output is not a promised wire-preservation contract.

Each accepted call invokes its upstream tool once. Retries, automatic MRTR,
reconnection and fallback execution are disabled. Outbound HTTP also rejects the
SDK's legacy initialization/cancel fallback before I/O, keeping initial failure
cleanup inside the connection budget. A timeout or broken connection
may leave execution outcome uncertain; the gateway never replays it. Unknown
tools and invalid arguments return sanitized MCP errors before invocation. Tool
failures retain `isError=true`. Transport failures use stable MCP reason messages
such as `upstream_unavailable`, `upstream_deadline_exceeded`, and
`request_cancelled`, rather than a generic HTTP JSON wrapper.

Fatal observed network loss latches readiness false and rejects later calls;
restart the gateway after restoring the fixture. Caller cancellation and ordinary
tool deadlines do not mark a healthy upstream permanently unavailable. No active
background upstream monitoring is performed.

Every present `Origin` must exactly match the gateway's configured
`http://IP:port` (or the fixture's own address). Missing Origin is allowed for native
clients; empty, repeated, `null`, other-host and other-port origins are rejected.
No wildcard CORS is enabled. The SDK checks protocol headers and metadata.
`curl GET /mcp` returns 405 and is not an MCP connectivity test.

## Timeouts and shutdown

The server retains header, body-read and idle timeouts. Its global write timeout
is disabled so it cannot truncate MCP streams at the old fixed deadline. The
HTTP middleware instead sets per-request write deadlines:

- Health and other routes: `WRITE_TIMEOUT`.
- MCP: `READ_TIMEOUT + MCP_CALL_TIMEOUT + WRITE_TIMEOUT`, covering body arrival,
  tool execution and final response writing. Execution itself is separately
  limited by `MCP_CALL_TIMEOUT` and inbound cancellation.

The response recorder supports flushing and `ResponseController` unwrapping;
there is no buffering timeout handler. Panic recovery never appends JSON after a
response or SSE stream is committed.

SIGINT/SIGTERM clears readiness and stops admission, then drains in-flight calls
while upstream resources stay available. HTTP draining and SDK cleanup share one
`SHUTDOWN_TIMEOUT` deadline. At expiry, remaining HTTP connections close and the
bridge cancels upstream work and closes its SDK client and idle HTTP connections.
Clean shutdown exits zero; configuration, startup, serve or forced-shutdown
failures exit nonzero. No subscriptions or hijacked transports are supported.

## Logs

JSON logs go to stdout. Each has `time`, `level`, `msg`, and `service`.
Gateway-generated `X-Request-ID` values replace caller IDs and correlate HTTP
access and MCP forwarding events.

`request_completed` records request ID, method, bounded route label, HTTP status
and duration. `mcp_call_completed` additionally records configured `upstream_id`,
exposed `tool`, `original_tool`, `outcome`, `reason_code` and duration. Outcomes
include `succeeded`, `tool_error`, `invalid_request`, `upstream_unavailable`,
`deadline_exceeded` and `cancelled`. HTTP 200 alone does not mean a tool succeeded.
Unknown tool names are logged as `unknown`, not attacker-supplied strings.

Logs exclude Authorization/Cookie headers, arbitrary incoming headers, arguments,
results, query strings, panic values and protocol session identifiers. SDK verbose
logging is disabled. Lifecycle logs include `upstream_ready`, `starting`,
`listening`, `shutdown_started`, `shutdown_completed`, and sanitized failure
messages. Informational logs are suppressed at `warn`/`error` levels. No identity
verification or policy decision is claimed.

## Verification

```sh
go fmt ./...
go vet ./...
go test ./...
go test -race ./...
go build -o ./bin/gateway ./cmd/gateway
go build -o ./bin/demo-upstream ./cmd/demo-upstream
go build -o ./bin/demo-client ./cmd/demo-client
```

The race detector requires a supported platform and C compiler. Tests cover the
foundation plus real loopback HTTP on both links: discovery/pagination, schemas,
echo/error results, raw argument preservation, invalid calls, header/Origin/body
rejections, cancellation, deadlines, network loss, concurrency and shutdown.

**Verified locally on 2026-10-17:** formatting, dependency tidy, vet, the full test
suite, race detection and all three builds passed on Go 1.26.5 darwin/amd64.
The suite also builds and runs the actual demo binaries and checks SIGINT/SIGTERM
with both draining and stuck calls. See [verification evidence](docs/step-2-verification.md)
for commands, test mappings, compatibility decisions and remaining limits.

## Layout and handoff

```text
cmd/gateway/                service composition and process signals
cmd/demo-upstream/          harmless local MCP fixture
cmd/demo-client/            discover/call/assert smoke client
internal/config/           environment validation
internal/gateway/          HTTP routing, middleware, lifecycle and integration tests
internal/mcpbridge/         SDK connection, immutable catalog and tool forwarding
internal/mcphttp/           shared local transport configuration and boundary
internal/demotools/         echo fixture definition
internal/requestid/         correlation context shared across HTTP and MCP
```

The source [Step 2 Notion plan](https://app.notion.com/p/3fcc64d519968184bd7cc88cc04aefd8)
is snapshotted in `docs/step-2-plan.md`; `docs/step-1-plan.md` preserves the prior
milestone. Detailed plans remain in Notion. No later milestone plan was created.

The local module remains `cyberforge/gateway`. Confirm the team's canonical
repository path before publishing; Git currently resolves to an unrelated parent
Desktop repository, and all project changes remain in this directory. Public
deployment and sensitive upstream tools are explicitly outside Step 2.
