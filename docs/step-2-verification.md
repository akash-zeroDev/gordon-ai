# Step 2 verification

Verified locally on 2026-10-17 with `go1.26.5 darwin/amd64`.
Source: [Step 2 Notion plan](https://app.notion.com/p/3fcc64d519968184bd7cc88cc04aefd8).

## Commands and observed results

| Check | Result |
| --- | --- |
| Existing Step 1 `go vet ./...` and `go test ./...` before implementation | Passed |
| `gofmt -l cmd internal` | Empty output: all Go files formatted |
| `go mod tidy` | Passed; exact SDK and validator dependencies/checksums recorded |
| `go vet ./...` | Passed |
| `go test -timeout 180s ./...` | Passed; gateway package 14.960s |
| `go test -race -timeout 180s ./...` | Passed; gateway package 15.912s; no reported data races |
| `go test -race -count=5 -run TestMCPStartupFailures -timeout 60s ./internal/gateway` | Passed; connection/discovery failures stay bounded |
| `go build -o ./bin/gateway ./cmd/gateway` | Passed |
| `go build -o ./bin/demo-upstream ./cmd/demo-upstream` | Passed |
| `go build -o ./bin/demo-client ./cmd/demo-client` | Passed |

Go registry/cache and local sockets required approved execution outside the
restricted sandbox. Dependencies were pinned without upgrading the toolchain.

## Acceptance evidence

| Behavior | Test evidence |
| --- | --- |
| Configuration defaults, overrides, empty/invalid settings, loopback URL restrictions | `internal/config/config_test.go` |
| Immutable definitions, duplicate/invalid catalog rejection, exact numeric argument validation | `internal/mcpbridge/catalog_test.go` |
| Paginated discovery, translated names and parameter headers, unchanged echo, structured output, tool error, raw arguments, no extra invocation | `TestMCPRoundTripAndFailures` |
| Unknown/invalid calls never invoke tools; credentials and payloads absent from logs | `TestMCPRoundTripAndFailures` |
| Origin, size/chunked size, unsupported version, method/name/parameter mismatch and malformed JSON rejection | `TestMCPTransportRejections` |
| Cooperative timeout and caller cancellation, one invocation, later requests still usable | `TestMCPDeadlineAndCancellation` |
| Observed upstream loss produces sanitized error and latches readiness false | `TestMCPDisconnectLatchesNotReady` |
| Concurrent response association | `TestMCPConcurrentCalls` |
| Clean draining and forced HTTP/SDK cleanup | `TestMCPDrainAndForcedShutdown` |
| Missing, stalled, redirecting or empty upstream fails bounded startup | `TestMCPStartupFailures` |
| JSON/SSE parsing through middleware, flush before completion, no recovery JSON appended to SSE | `internal/gateway/streaming_test.go` |
| MCP response outlives the old short write deadline | `TestMCPOutlivesOldWriteDeadline` |
| Actual fixture/gateway/client binaries: direct and bridged smoke success, health, occupied port, missing upstream, invalid config | `TestBuiltBinaries/documented_demo` |
| Actual gateway binary: SIGINT and SIGTERM each drain a short call (exit zero) and bound a stuck call (nonzero), without replay | `TestBuiltBinaries/signal=...` |
| HTTP health, readiness, correlation, recovery and lifecycle regressions | `internal/gateway/server_test.go` |

The executable smoke client confirmed:

```text
PASS: discovered demo.echo; echo matched; protocol 2026-07-28
```

## Compatibility decisions and limits

- MCP SDK: `github.com/modelcontextprotocol/go-sdk v1.8.0`; protocol `2026-07-28`.
- The SDK regenerates protocol metadata. Gateway application metadata filtering
  deliberately permits only the `com.cyberforge/` result namespace.
- `github.com/santhosh-tekuri/jsonschema/v6 v6.0.3` validates exact JSON numbers;
  testing found the SDK's schema validator classified `json.Number` as a string.
- Initial discovery cancellation could trigger the SDK's legacy cleanup path and
  exceed the connection budget. The outbound transport now rejects legacy
  operations before I/O; the regression passed five race-enabled runs.
- Argument JSON is forwarded without numeric conversion. Structured output uses
  the SDK's decoded representation; arbitrary-precision numeric output fidelity
  is not promised. The controlled string echo is verified unchanged.
- This is one unauthenticated, loopback-only upstream and a fixed startup
  catalog. Restart after catalog changes or fatal upstream loss. No JWT/OAuth,
  policies, AI inspection, subscription forwarding or backend integration.
- Repository publication/module identity remains a team handoff item. Nothing
  was committed, pushed or deployed to the unrelated parent Desktop repository.
