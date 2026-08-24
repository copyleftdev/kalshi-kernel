<picture>
  <source media="(prefers-color-scheme: dark)" srcset="./assets/brand/kernel-lockup-dark.svg">
  <source media="(prefers-color-scheme: light)" srcset="./assets/brand/kernel-lockup-light.svg">
  <img src="./assets/brand/kernel-lockup-light.svg" alt="Kernel" width="360">
</picture>

# Kalshi Kernel

An unofficial, safety-focused Model Context Protocol (MCP) server for Kalshi
event contracts and perpetual markets. This is a **pre-release** project that
is **not ready for trading**. The server is designed for agent
harnesses such as Hermes and exposes a deliberately small tool surface generated
from Kalshi's published API contracts.

Canonical publisher: **[CopyleftDev](https://github.com/copyleftdev)**. Planned
source and container releases use `github.com/copyleftdev/kalshi-kernel` and
`ghcr.io/copyleftdev/kalshi-kernel` respectively.

> [!CAUTION]
> **Pre-release software that can submit real orders.** Published tags are
> conformance-tested scaffolds; the current `main` branch adds market-data,
> paper-trading, and live event-contract adapters. Configured live mode can
> place, amend, and cancel real orders. Review the exact
> commit, configuration, tests, and limits before running it with credentials;
> do not infer readiness from the version string.

> [!IMPORTANT]
> This community project is not an official Kalshi product and is not affiliated
> with, endorsed by, sponsored by, or supported by KalshiEX LLC or its
> affiliates. “Kalshi” is used only to identify interoperability with Kalshi's
> published APIs.

The independent Kernel mark follows a conservative interpretation of Kalshi's
published color, contrast, clear-space, and misuse guidance. It does not use or
modify Kalshi's logo. See the [visual identity usage rules](assets/brand/README.md).

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

The table describes the current `main` branch, not the published `v0.1.1` tag.
The additions are unreleased and the version metadata has not yet advanced.

| Area | Current behavior and limits |
| --- | --- |
| MCP stdio transport and generated schemas | Working |
| Public market data | Event contracts and perpetuals: search, market details, REST order-book snapshots, candles, and last quote. Public trade tape is event-contract only |
| Paper trading | In-memory balance, positions, fill journal, exact fixed-point fees, and immediate all-or-nothing fills at the current displayed touch |
| Live portfolio | Authenticated event-contract balance, positions, orders, and fills. Perpetual accounts and non-primary subaccounts are not supported |
| Live event-contract orders | Place, amend, and cancel are connected to the production API. Place/amend require process-local arming and kernel-side limits; cancel performs a resting-state check and timeout reconciliation |
| Live perpetual orders | Not implemented |
| Streaming order-book reconciliation | Not implemented; `get_orderbook` currently returns one public REST snapshot |
| `kernel_status` | Callable, but some readiness and arming fields still describe the old scaffold and must not be treated as authoritative |
| Automatic upstream specification freshness gate | Working |
| Production remote HTTPS/OAuth service | Not implemented |
| Public registry listings | Prepared, not submitted |

Treat the current branch as development software, not as a published or audited
trading integration. Some design and publication documents still describe the
`v0.1.1` scaffold; compare them with the implementation before relying on a
claim. See [ARCHITECTURE.md](docs/ARCHITECTURE.md),
[THREAT_MODEL.md](docs/THREAT_MODEL.md), and the release gates in
[PUBLICATION.md](docs/PUBLICATION.md).

## Agent-facing tools

| Tool | Class | Current behavior |
| --- | --- | --- |
| `kernel_status` | Read-only | Returns mode and a readiness envelope; newly added backend and arm state are not yet reflected accurately |
| `search_markets` | Read-only | Searches event-contract or perpetual markets through public REST |
| `get_market` | Read-only | Returns authoritative metadata for one event-contract or perpetual market |
| `get_orderbook` | Read-only | Returns a public REST snapshot of yes/no bid levels for either product |
| `get_candles` | Read-only | Returns 1-, 60-, or 1440-minute event-contract or perpetual OHLC buckets |
| `get_trades` | Read-only | Returns one cursor-paginated page of the public event-contract trade tape |
| `get_last` | Read-only | Returns a compact event-contract or perpetual quote and status snapshot |
| `arm_live_trading` | Destructive/control | Arms or disarms live place/amend calls for this process after an exact second acknowledgement |
| `get_portfolio` | Read-only | Returns the local paper ledger in paper mode or the authenticated event-contract portfolio in live mode |
| `place_order` | Destructive/write | Simulates an immediate fill in paper mode; submits an armed, capped event-contract order in live mode |
| `amend_order` | Destructive/write | Paper mode has no resting orders; live mode amends an armed, resting event-contract order |
| `cancel_order` | Destructive/write | Paper mode has no resting orders; live mode state-checks and cancels a resting event-contract order |

Connected MCP clients also receive server-level instructions telling them to
call `kernel_status` first, distinguish paper from live mode, and never report
an action as successful unless its structured response contains `ok: true`.
Until its readiness reporting is updated, confirm live arm state from the
`arm_live_trading` response rather than from `kernel_status`.

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

Paper mode uses an in-memory simulated ledger and never submits an order to
Kalshi. It starts with `$100.00` by default; set
`KALSHI_PAPER_CASH_DOLLARS` before startup to choose another balance. The
ledger, positions, idempotency records, and fill journal reset when the process
exits.

`place_order` fetches a fresh public order-book snapshot and supports only
immediate, all-or-nothing marketable fills. The requested price must exactly
equal the current touch and the displayed size must cover the full quantity.
The simulator uses fixed-point arithmetic, applies the published fee formula,
records a hash of the book used, and treats `client_order_id` as an idempotency
key. Because paper orders never rest, `amend_order` and `cancel_order` return
typed failures.

Every paper response is labeled `simulated: true`. Simulated fills do not
predict or guarantee live fills, liquidity, latency, slippage, fees, or
profitability.

### Live mode

Live mode is deliberately difficult to enable:

```sh
KALSHI_KERNEL_MODE=live
KALSHI_API_KEY_ID=...
KALSHI_PRIVATE_KEY_PATH=/absolute/path/to/private-key.pem
KALSHI_LIVE_TRADING_ACK=I_UNDERSTAND_THIS_TRADES_REAL_MONEY

# Optional startup-only overrides; these are the defaults.
KALSHI_MAX_ORDER_NOTIONAL_DOLLARS=25.00
KALSHI_MAX_DAILY_NOTIONAL_DOLLARS=100.00
KALSHI_MAX_DAILY_ORDERS=200
```

Startup configuration selects immutable live mode and enables authenticated
event-contract portfolio reads and cancellation. It does **not** authorize
place/amend calls. Those calls require a second, process-local MCP action:

```json
{
  "acknowledgement": "I_UNDERSTAND_THIS_TRADES_REAL_MONEY",
  "arm": true
}
```

Pass that object to `arm_live_trading`; pass the same object with `arm: false`
to disarm. Arming is never persisted. `place_order` also requires a stable,
caller-provided `client_order_id`. Place/amend requests are rejected before
submission when they exceed the startup-only per-order, UTC-day notional, or
UTC-day order-count limits. These counters are process-local and reset when the
process restarts.

> [!WARNING]
> `cancel_order` is available in configured live mode without the separate
> `arm_live_trading` step. It verifies that the order is resting before sending
> the cancellation. Place/amend validate price syntax and notional limits, but
> the current implementation does not independently compare the requested limit
> price with a fresh quote. Use exchange-side limits, least-privilege credentials,
> human review, monitoring, and an independent emergency stop.

Live account reads and writes currently support event contracts only.
Perpetual-account operations and non-primary subaccounts are unavailable; omit
`subaccount` or pass `0`. Network failures after a write produce reconciled or
explicitly indeterminate outcomes rather than blind retries.

Never commit credentials, private keys, or `.env` files. Use short-lived
credentials where available, a secrets manager in production, and an account
with only the privileges and capital required.

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
