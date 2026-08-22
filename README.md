# Kalshi Kernel

An unofficial, safety-focused Model Context Protocol (MCP) server for Kalshi
event contracts and perpetual markets. The server is designed for agent
harnesses such as Hermes and exposes a deliberately small tool surface generated
from Kalshi's published API contracts.

Canonical publisher: **[CopyleftDev](https://github.com/copyleftdev)**. Planned
source and container releases use `github.com/copyleftdev/kalshi-kernel` and
`ghcr.io/copyleftdev/kalshi-kernel` respectively.

> [!CAUTION]
> **Pre-release software—not ready for trading.** Version `0.1.1` is a
> conformance-tested scaffold. Only `kernel_status` is operational. Market-data,
> portfolio, paper-trading, and live-trading adapters are not connected and fail
> closed with `capability_not_ready`.

> [!IMPORTANT]
> This community project is not an official Kalshi product and is not affiliated
> with, endorsed by, sponsored by, or supported by KalshiEX LLC or its
> affiliates. “Kalshi” is used only to identify interoperability with Kalshi's
> published APIs.

## Why this project exists

Trading APIs are broad and optimized for application developers. Agents need a
narrower boundary with explicit intent, strict schemas, predictable errors, and
strong separation between simulation and real-money execution. Kalshi Kernel
provides that boundary while continuously testing its generated clients against
the latest upstream contracts.

The design priorities are:

- paper mode by default;
- immutable execution mode for the lifetime of a process;
- explicit acknowledgement and credentials before live mode can arm;
- fixed-point strings for prices and quantities;
- strict, generated tool schemas with safety annotations;
- fail-closed behavior when a capability is unavailable;
- traceable alignment with exact OpenAPI operations and AsyncAPI channels; and
- repeatable MCP protocol, race, fuzz, and upstream-drift testing.

## Current status

| Area | Status in 0.1.1 |
| --- | --- |
| MCP stdio transport | Working |
| MCP schemas, titles, annotations, and instructions | Working |
| `kernel_status` | Working |
| Kalshi REST and WebSocket client generation | Working |
| Automatic upstream specification freshness gate | Working |
| Paper ledger and fill simulator | Not implemented |
| Market-data and portfolio adapters | Not implemented |
| Live order execution | Not implemented |
| Production remote HTTPS/OAuth service | Not implemented |
| Public registry listings | Prepared, not submitted |

Do not advertise, deploy, or rely on this version as a functioning trading
integration. See [ARCHITECTURE.md](docs/ARCHITECTURE.md),
[THREAT_MODEL.md](docs/THREAT_MODEL.md), and the release gates in
[PUBLICATION.md](docs/PUBLICATION.md).

## Agent-facing tools

| Tool | Class | Current behavior |
| --- | --- | --- |
| `kernel_status` | Read-only | Returns mode and backend readiness |
| `search_markets` | Read-only | Fails closed until adapter exists |
| `get_market` | Read-only | Fails closed until adapter exists |
| `get_orderbook` | Read-only | Fails closed until adapter exists |
| `get_portfolio` | Read-only | Fails closed until adapter exists |
| `place_order` | Destructive/write | Fails closed until adapter exists |
| `amend_order` | Destructive/write | Fails closed until adapter exists |
| `cancel_order` | Destructive/write | Fails closed until adapter exists |

Connected MCP clients also receive server-level instructions telling them to
call `kernel_status` first, distinguish paper from live mode, and never report
an action as successful unless its structured response contains `ok: true`.

## Build and run locally

Prerequisites:

- Go 1.25.13 or newer;
- Node.js 22 only for the official MCP conformance runner; and
- a supported MCP client.

```sh
make test
make build
KALSHI_KERNEL_MODE=paper ./bin/kalshi-kernel
```

Paper mode is the default when `KALSHI_KERNEL_MODE` is unset. It intentionally
discards live credential configuration.

### Hermes

Add a local stdio server to `~/.hermes/config.yaml`:

```yaml
mcp_servers:
  kalshi-kernel:
    command: "/absolute/path/to/kalshi-kernel/bin/kalshi-kernel"
    env:
      KALSHI_KERNEL_MODE: "paper"
```

Restart Hermes and call `kernel_status` before any other tool.

### Claude Code

After building the binary:

```sh
claude mcp add --scope user --transport stdio kalshi-kernel \
  --env KALSHI_KERNEL_MODE=paper -- /absolute/path/to/bin/kalshi-kernel
```

Then run `claude mcp get kalshi-kernel` or open `/mcp` to confirm the
connection. This local setup is separate from Anthropic's public Connectors
Directory, which requires a deployed remote server.

### Other stdio clients

Use the standard MCP configuration shape:

```json
{
  "mcpServers": {
    "kalshi-kernel": {
      "command": "/absolute/path/to/bin/kalshi-kernel",
      "env": {
        "KALSHI_KERNEL_MODE": "paper"
      }
    }
  }
}
```

## Execution modes

### Paper mode

```sh
KALSHI_KERNEL_MODE=paper ./bin/kalshi-kernel
```

Paper mode will use a local simulated ledger after that backend is implemented.
It must never submit an order to Kalshi. Simulated fills will not predict or
guarantee live fills, liquidity, latency, slippage, fees, or profitability.

### Live mode

Live mode is deliberately difficult to enable:

```sh
KALSHI_KERNEL_MODE=live
KALSHI_API_KEY_ID=...
KALSHI_PRIVATE_KEY_PATH=/absolute/path/to/private-key.pem
KALSHI_LIVE_TRADING_ACK=I_UNDERSTAND_THIS_TRADES_REAL_MONEY
```

These variables currently arm configuration validation only; no live execution
adapter is connected. Never commit credentials, private keys, or `.env` files.
Use short-lived credentials where available, a secrets manager in production,
and an account or subaccount with the least privileges and capital required.

## Specification-driven generation

The curated agent interface lives in `specs/mcp-tools.yaml`. It maps each MCP
tool to exact OpenAPI operation IDs and AsyncAPI channels. Administrative and
account-management endpoints are not automatically exposed merely because they
exist upstream.

Four authoritative contracts are fetched from `https://docs.kalshi.com`:

- `openapi.yaml` — event-contract REST API;
- `asyncapi.yaml` — event-contract WebSocket API;
- `perps_openapi.yaml` — perpetuals REST API; and
- `perps_asyncapi.yaml` — perpetuals WebSocket API.

The fetcher pins HTTPS and the source hostname, limits response sizes, validates
the expected dialect and non-empty surface, and records hashes and HTTP
provenance. The upstream contract bodies are ignored by Git and fetched into a
local cache as needed. Upstream-derived REST clients are also generated locally
and excluded from source releases. Only the provenance lock, curated MCP
overlay, and generated MCP tool boundary are versioned. Regeneration and testing
happen in a temporary repository copy before a tested lock and MCP boundary can
be promoted.

```sh
make upstream-test      # test latest contracts without requiring snapshot parity
make upstream-check     # require parity and run MCP conformance
make upstream-promote   # promote only the exact artifacts that passed
```

Every CI run performs the strict upstream check; CI also runs daily so drift is
detected even when the repository is idle.

> [!WARNING]
> The downloaded Kalshi contract files identify at least part of the upstream
> material as proprietary. They are not covered by this project's Apache-2.0
> license and must not be committed or redistributed. Review
> [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md) before publishing generated
> artifacts.

## Verification

```sh
make test              # unit, integration, and contract alignment tests
make test-alignment    # OpenAPI, AsyncAPI, and MCP overlay alignment
make check-generated   # deterministic generated-code check
make test-race         # concurrent calls under Go's race detector
make test-fuzz         # execution-mode parser safety fuzzing
make conformance       # pinned official MCP protocol scenarios
make release-test      # complete local release gate
make public-check      # vulnerability, metadata, and public-readiness gate
make upstream-check    # latest Kalshi contracts plus conformance
```

The suite checks transport security, authentication contracts, mutation
security, operation-ID uniqueness, fixed-point types, curated surface area,
required-field parity, strict JSON Schemas, tool titles and safety hints,
fail-closed behavior, concurrency, and Streamable HTTP negotiation.

## Distribution and registries

The repository contains publication metadata for the official MCP Registry and
submission dossiers for the OpenAI Plugins Directory and Anthropic directories.
Metadata is preparation—not evidence of approval, endorsement, or publication.

Anthropic's current connector review criteria do not accept connectors that
transfer money or other financial assets. Any Claude Connectors Directory build
of this project must therefore be a distinct read-only and/or paper-only
artifact with live order tools omitted, subject to Anthropic's review.

See [PUBLICATION.md](docs/PUBLICATION.md) for the exact channel matrix, current
blockers, release steps, listing copy, and review test cases. No external
registry submission is performed automatically.

## Contributing and security

Read [CONTRIBUTING.md](CONTRIBUTING.md) before opening a pull request. Report
vulnerabilities privately according to [SECURITY.md](SECURITY.md); do not open a
public issue for a suspected credential leak or trading-safety vulnerability.
General usage help belongs in [SUPPORT.md](SUPPORT.md).

## Legal and risk notice

This software is provided for development and research. It does not provide
investment, financial, legal, tax, compliance, or trading advice. Event
contracts and leveraged products can result in rapid and substantial loss,
including loss of the entire amount committed. You are responsible for account
eligibility, jurisdictional restrictions, exchange rules, regulatory
requirements, taxes, strategy, orders, and losses.

AI systems and software can misunderstand intent, produce incorrect parameters,
repeat requests, or behave unexpectedly. Human review, exchange-side risk
limits, least-privilege credentials, monitoring, and an independent emergency
stop are required before any live deployment.

Read the full [DISCLAIMER.md](DISCLAIMER.md), [PRIVACY.md](PRIVACY.md), and
[SECURITY.md](SECURITY.md). The project is licensed under
[Apache License 2.0](LICENSE); third-party material is excluded as described in
[THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md).
