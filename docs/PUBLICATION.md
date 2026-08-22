# Publication plan

This document separates files that can be prepared in source control from
external actions that require a publisher identity, accounts, infrastructure,
legal review, and explicit release authorization.

## Release posture

Version `0.1.1` is a pre-release scaffold. It must not be submitted to a public
directory as a functional trading connector because only `kernel_status` is
operational. Registry metadata in this repository is preparatory and does not
claim review, approval, endorsement, or availability.

The canonical release owner is **CopyleftDev**. Source releases target
`github.com/copyleftdev/kalshi-kernel`, container releases target
`ghcr.io/copyleftdev/kalshi-kernel`, and the MCP Registry namespace is
`io.github.copyleftdev/kalshi-kernel`.

## Channel matrix

| Channel | Artifact or submission path | Prepared here | Blocking work |
| --- | --- | --- | --- |
| Official MCP Registry | `server.json` plus GHCR OCI image | Metadata and image recipe | Public GitHub/GHCR release, namespace ownership, publication login |
| OpenAI Plugins Directory | OpenAI submission portal | Listing/test dossier | Functional tools, production HTTPS `/mcp`, auth, verified publisher, domain, legal URLs, reviewer account |
| Anthropic Connectors Directory | Claude.ai directory portal | Read-only/paper listing and checklist dossier | Remove live financial-asset transfer tools from the submitted build; functional remote server, HTTPS, OAuth, legal URLs, reviewer account |
| Claude Plugin Directory | Public GitHub plugin submission | Disabled-by-default manifest and setup skill | Publish portable binary/package, validate with Claude CLI, complete functional tools |
| Claude Desktop Extensions | MCPB submission form | Packaging research only | Signed cross-platform MCPB artifacts and privacy manifest |
| GitHub Releases | Version-tag workflow | Build workflow | Public repository and release authorization |
| GitHub Container Registry | Version-tag workflow | OCI recipe and labels | Public repository/package permissions and release authorization |

## Blocking publication checklist

All boxes must be resolved before a public directory submission:

- [ ] Implement and integration-test market-data and portfolio tools.
- [ ] Implement a deterministic paper ledger and fill simulator.
- [ ] Complete a separate reviewed gate before enabling live order tools.
- [x] Exclude Kalshi's proprietary specification bodies from the Git release.
- [x] Exclude upstream-derived REST clients from the current source and binary release.
- [ ] Complete a rights review before a future release distributes those clients.
- [ ] Establish the legal publisher identity and confirm Apache-2.0 is intended.
- [ ] Establish a monitored security, privacy, and user-support contact.
- [ ] Publish final HTTPS privacy policy, terms, support, and documentation URLs.
- [ ] Deploy a stable production Streamable HTTP endpoint for remote directories.
- [ ] Implement per-user authentication and authorization; never share a process
      configured with one user's Kalshi credentials among unrelated users.
- [ ] Complete threat modeling, external security review, load testing, audit
      logging, rate limiting, monitoring, rollback, and incident response.
- [ ] Provide reviewer fixtures and credentials that require no MFA or private
      network access.
- [ ] Confirm geographic, regulatory, exchange, financial-transaction, and model
      provider policy eligibility with qualified counsel and each platform.
- [ ] Produce separate capability manifests/builds where a directory prohibits
      live financial-asset transfers; never rely on a runtime flag to hide an
      ineligible tool from reviewers or users.
- [ ] Run every positive and negative review case against the deployed build.

## Official MCP Registry

The registry is a metadata registry; it does not host packages. `server.json`
describes the intended `ghcr.io/copyleftdev/kalshi-kernel:0.1.1` OCI artifact.
The image must exist publicly and carry the matching
`io.modelcontextprotocol.server.name` label before publication.

Release sequence:

1. Make the repository public at the URL declared in `server.json`.
2. Resolve the third-party specification and publisher-identity blockers.
3. Run `make public-check`, `make release-test`, and `make upstream-check`.
4. Tag the exact reviewed commit as `v0.1.1`.
5. Verify release checksums, provenance, OCI label, platforms, and stdio startup.
6. Authenticate with the official `mcp-publisher` using the GitHub identity that
   owns the `io.github.copyleftdev/*` namespace.
7. Run the publisher's validation/dry-run facility available at release time.
8. Publish `server.json`, then query the registry API and verify the listing.

Do not add a remote entry to `server.json` until a stable public endpoint exists.

## OpenAI Plugins Directory

Official OpenAI documentation currently requires a stable, public HTTPS MCP
endpoint using Streamable HTTP for public MCP-backed plugin submission. It also
requires a verified developer or business identity, matching public website,
support, privacy, and terms URLs, domain verification, accurate tool metadata,
starter prompts, and at least five positive plus three negative review cases.

Prepared listing copy:

- **Name:** Kalshi Kernel
- **One-line description:** Inspect Kalshi markets and manage explicitly
  authorized paper or live orders through a safety-focused MCP boundary.
- **Category:** Finance / Developer tools
- **Disclosure:** Unofficial community integration; not affiliated with or
  endorsed by Kalshi.

Suggested starter prompts after the corresponding tools are implemented:

1. “Check the kernel status and tell me whether this connection is paper or live.”
2. “Find open event markets matching inflation and summarize, without trading.”
3. “Show the current order book and freshness metadata for this ticker.”
4. “Summarize my paper portfolio and resting orders.”
5. “Preview this order and explain every parameter; do not submit it.”

Review cases must include at least these negative behaviors:

1. A request to claim an unavailable adapter succeeded returns a clear failure.
2. A live order without explicit user authorization is not submitted.
3. Invalid, ambiguous, stale, oversized, or out-of-range order input is rejected.
4. A prompt-injection instruction found in external market content cannot change
   execution mode or bypass confirmation.
5. A retry cannot create a duplicate order.

The current code cannot pass the positive functional cases and therefore must
not be submitted yet.

## Anthropic Connectors Directory

Anthropic currently accepts remote MCP servers through its directory submission
portal. Requirements include HTTPS, supported transport, clear setup docs,
accurate titles and read/destructive annotations, privacy information, and
OAuth 2.0 for authenticated services unless another review-supported connection
mode applies. Submission requires the appropriate Claude Team/Enterprise role.

Anthropic's current pre-submission checklist lists connectors that transfer
money, cryptocurrency, or other financial assets as unsupported. A directory
submission for this project must therefore expose only read-only market data
and/or isolated paper simulation. This project treats live order placement as
falling within that restriction unless Anthropic provides explicit written
guidance otherwise. The directory artifact must omit live tools entirely; a
disabled runtime mode is not an adequate representation of the submitted tool
surface.

Use the same listing disclosure and negative test corpus as the OpenAI review.
The portal also asks explicitly about financial transactions, third-party APIs,
data handling, test credentials, and whether every tool has been run. Answer
these fields literally; fail-closed stubs are not completed tools.

Anthropic's separate Claude Plugin Directory accepts public GitHub plugins. The
repository includes a disabled-by-default plugin manifest and a paper-only MCP
configuration. The configuration expects a verified `kalshi-kernel` binary on
`PATH`; the setup skill never downloads or executes an unverified binary.

Do not submit the plugin until signed/checksummed per-platform binaries or an
equivalent portable package exist and functional tools pass the review corpus.
A plugin that silently downloads or builds code at startup is not the desired
public user experience.

## Version updates

Keep these values synchronized:

- `VERSION`;
- `specs/mcp-tools.yaml` server version;
- `server.json` server and OCI package version;
- `.claude-plugin/plugin.json` and `.claude-plugin/marketplace.json`;
- Git tag and release artifact version; and
- changelog heading.

Published tool names and schemas are an API. Prefer additive changes. Any
breaking change requires a versioning and migration plan plus fresh OpenAI and
Anthropic review where applicable.

## Authoritative submission references

- [Official MCP Registry publishing quickstart](https://modelcontextprotocol.io/registry/quickstart)
- [Official MCP Registry remote-server format](https://modelcontextprotocol.io/registry/remote-servers)
- [OpenAI MCP server requirements](https://developers.openai.com/plugins/build/mcp-server)
- [OpenAI plugin submission requirements](https://developers.openai.com/plugins/deploy/submission)
- [Anthropic connector submission requirements](https://claude.com/docs/connectors/building/submission)
- [Anthropic connector pre-submission checklist](https://claude.com/docs/connectors/building/review-criteria)
- [Anthropic plugin submission requirements](https://claude.com/docs/plugins/submit)
- [Claude Code MCP configuration](https://code.claude.com/docs/en/mcp)
