# Contributing

Thank you for helping improve Kalshi Kernel. This project sits on a financial
execution boundary, so changes are reviewed more conservatively than ordinary
application code.

## Before contributing

- Read the README, disclaimer, security policy, and current publication gates.
- Never use or include real credentials, private keys, account data, or trading
  history in tests, issues, commits, logs, or screenshots.
- Open an issue before a large architectural, public API, execution-safety, or
  registry change.
- Report vulnerabilities privately under `SECURITY.md`.

## Development workflow

Prerequisites are Go 1.25.13+, Bash, and standard Unix tools. Node.js 22 is needed
for MCP conformance.

```sh
make generate
make test
make test-race
make test-alignment
make check-generated
make public-check
```

Before release-sensitive changes, also run:

```sh
make test-fuzz
make conformance
make upstream-check
```

`upstream-check` uses network access to download current official Kalshi
contracts and the pinned MCP conformance runner.

## Change requirements

Pull requests should be focused and explain:

- the problem and intended behavior;
- safety and failure-mode implications;
- tests added or changed;
- generated files and upstream operations affected;
- configuration, compatibility, privacy, and documentation changes; and
- a rollback approach for execution-path changes.

### MCP surface changes

Edit `specs/mcp-tools.yaml`, not generated MCP code. Every tool needs a stable
name, human-readable title, precise description, strict input schema, output
schema, and accurate annotations. The tool must map to explicit upstream
operations or channels. Regenerate with `make generate`.

Adding a tool is a security and product decision. Do not expose an upstream
administrative operation solely because code generation makes it available.

### Trading changes

Execution changes must remain bounded, deterministic, and fail closed. They
require tests for at least:

- paper/live isolation;
- authorization and explicit confirmation;
- fixed-point validation;
- idempotency and retry behavior;
- timeouts, partial failures, and reconciliation;
- stale market data;
- order, position, and loss limits; and
- logs that exclude secrets and unnecessary account data.

No live execution change should merge based only on mocked success paths.

### Generated code and specifications

Never edit `internal/gen/**` manually. `make check-generated` must pass. Upstream
contracts must be fetched only by the pinned `cmd/specfetch` source mapping.
Changes to vendored third-party specifications require a licensing review under
`THIRD_PARTY_NOTICES.md`.

## Commits and compatibility

Use clear, imperative commit subjects. Preserve backward compatibility for
published MCP tool names and required fields unless a major version explicitly
documents the break. Security fixes may intentionally narrow behavior.

By submitting a contribution, you agree that it is provided under Apache
License 2.0 and that you have the right to submit it. Do not contribute
third-party code or documentation without compatible licensing and attribution.
