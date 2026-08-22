# Architecture

Kalshi Kernel separates an agent-facing MCP contract from upstream exchange
contracts and from execution backends. This prevents upstream API breadth from
automatically becoming agent authority.

```text
MCP client / agent
        |
        | strict tool schema + safety annotations
        v
generated MCP registration boundary
        |
        | typed intent
        v
kernel policy and immutable execution mode
        |
        +-------------------+
        |                   |
        v                   v
paper adapter           live adapter
(planned)               (planned, separately gated)
        |                   |
        +---------+---------+
                  |
                  v
       generated Kalshi clients
        REST + WebSocket contracts
```

## Control plane: specifications and generation

`cmd/specfetch` downloads four fixed authoritative URLs from
`docs.kalshi.com`. It requires HTTPS, pins the host across redirects, bounds
response size, validates the contract dialect and minimum surface, and records
SHA-256 provenance.

`cmd/mcpgen` reads the curated `specs/mcp-tools.yaml` overlay. Generation fails
if an operation ID or channel disappears, schemas are malformed, titles or
descriptions are missing, safety classes are invalid, or numeric boundaries are
inconsistent. Generated files are committed only after isolated tests pass.

## Data plane: MCP requests

The production command currently serves MCP over stdio. Inputs cross these
boundaries:

1. The MCP SDK negotiates protocol capabilities and validates the generated
   strict JSON Schema.
2. Generated registration converts arguments to typed intent structures.
3. The kernel handler applies mode and readiness policy.
4. An adapter either performs the operation or returns a structured fail-closed
   error.
5. Results use a stable envelope containing mode, `ok`, data, and structured
   error information.

Only step 1–3 and `kernel_status` are operational in version 0.1.1. No exchange
adapter or paper ledger is connected.

## Execution-mode invariant

Mode is read once at startup and cannot be changed through MCP. Paper mode is
the default and drops production credential configuration. Live mode requires a
literal risk acknowledgement plus credential configuration. Even then,
`live_trading_armed` remains false until a reviewed backend is actually ready.

An eventual live adapter must additionally gate each consequential action on
authorization, confirmation, limits, current state, freshness, idempotency, and
reconciliation. Startup configuration alone is never sufficient authority to
trade.

## Transport boundary

The local server is stdio. `cmd/conformance-server` exposes loopback HTTP only
for tests and refuses non-loopback binds; it is not a production deployment
entry point.

A future hosted service for OpenAI or Anthropic directories requires a separate
production command and architecture with TLS termination, OAuth 2.0, per-user
authorization, tenant isolation, rate limits, audit logs, observability, and
incident controls. It must not reuse one local user's environment credentials
across requests or users.

## Compatibility policy

MCP tool names, required fields, and response envelopes become public API once
published. Prefer additive evolution. Upstream API changes are continuously
tested, but they do not automatically add MCP tools. Breaking MCP changes need a
major-version and migration review.
