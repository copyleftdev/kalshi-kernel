# Threat model

This is a living engineering document, not a security certification. It covers
the local pre-release architecture and identifies requirements for future
trading and hosted deployments.

## Assets

- Kalshi private keys and API key identifiers;
- account balances, positions, orders, fills, and subaccount identifiers;
- order intent, including ticker, side, price, quantity, and timing;
- execution mode and risk limits;
- specification and generated-client integrity;
- MCP prompts, tool arguments, results, and audit records; and
- release artifacts and registry identity.

## Trust boundaries

- agent/model to MCP server;
- MCP client to stdio process or future HTTPS endpoint;
- untrusted tool arguments to typed kernel intent;
- kernel to paper or live adapter;
- adapter to Kalshi REST/WebSocket APIs;
- upstream specifications to generated source code;
- source repository to CI, release artifacts, registries, and users; and
- in a future hosted service, one user or tenant to another.

## Threats and required controls

### Unintended or manipulated orders

Threats include ambiguous prompts, prompt injection in external content,
incorrect tool selection, stale context, repeated calls, and parameter
substitution.

Controls include strict schemas, explicit destructive annotations, immutable
mode, user confirmation, fixed-point validation, state re-fetch before writes,
idempotency keys, bounded retries, exchange-side limits, and post-action
reconciliation. Only the schema/mode/fail-closed subset exists today.

### Paper/live boundary bypass

Paper mode must have no code path capable of reaching a production write
endpoint or using production credentials. Tests must inject production-looking
credentials in paper mode and prove they are discarded. Mode may not be changed
through a tool call.

### Credential disclosure or misuse

Private keys must never enter prompts, MCP arguments, structured results,
repository files, images, logs, or container layers. Live deployments need
restrictive file permissions, secret management, key rotation, least privilege,
and an out-of-band revocation path.

### Duplicate or uncertain execution

Network timeouts can leave an order's outcome unknown. Never treat a timeout as
a clean failure and blindly retry. Use stable client order IDs, query the
exchange, reconcile state, and return an explicit indeterminate outcome until
resolved.

### Stale or inconsistent market data

Order-book state must carry source, timestamp, sequence, and freshness metadata.
Gaps require snapshot reconciliation. Trading policy must reject data outside
configured freshness bounds.

### Specification supply-chain compromise

The fetcher pins HTTPS and `docs.kalshi.com`, limits redirects and size,
validates dialect/surface, and records hashes. Generation and all tests run in
isolation before promotion. Remaining risks include compromise of the trusted
origin, malicious but schema-valid changes, dependency compromise, and reviewer
error. Human semantic review is required for every upstream diff.

### Package and registry compromise

Release artifacts need reproducible versioning, checksums, provenance, an SBOM,
least-privilege CI permissions, protected tags, and verified registry
namespaces. Users must be able to trace a package to a reviewed source commit.

### Hosted-service tenant isolation

No hosted multi-user implementation exists. Before one is built, design and
test OAuth token isolation, per-user Kalshi credential custody, authorization on
every tool call, encrypted storage, deletion/retention, request limits, audit
access controls, cross-tenant tests, and incident response. A process-level
environment credential model is unsuitable for a public multi-user service.

## Explicitly out of scope for 0.1.1

- claims of live-trading safety or availability;
- custody of user credentials by a project-operated service;
- regulatory or jurisdictional compliance certification;
- protection against a compromised host or MCP client; and
- profitability, strategy correctness, or market integrity.

Any change that brings one of these into scope requires a new threat-model
review before release.
