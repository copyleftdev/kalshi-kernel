# Changelog

All notable changes will be documented here. The project follows Semantic
Versioning after its first stable release. During `0.x`, minor versions may
contain compatibility changes, which will be called out explicitly.

## [Unreleased]

### Added

- Public-release policies, risk disclosures, contribution guidance, and
  registry preparation.
- MCP tool titles and server-level safety instructions.

## [0.1.1] - 2026-08-22

### Fixed

- Encode the OCI version only in the package identifier, as required by the
  live MCP Registry publisher.
- Exclude proprietary upstream contract bodies and derived REST clients from
  the public Git release while preserving dynamic generation and freshness
  tests.

## [0.1.0] - 2026-08-22

### Added

- Spec-generated Go clients for Kalshi event-contract and perpetual APIs.
- Curated eight-tool MCP surface with strict schemas and safety annotations.
- Paper-by-default and explicitly acknowledged live-mode configuration.
- Fail-closed handlers while execution adapters remain unavailable.
- Official MCP protocol conformance, race, fuzz, alignment, and drift tests.
- Dynamically fetched upstream specification build and promotion workflow.

[Unreleased]: https://github.com/copyleftdev/kalshi-kernel/compare/v0.1.1...HEAD
[0.1.1]: https://github.com/copyleftdev/kalshi-kernel/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/copyleftdev/kalshi-kernel/releases/tag/v0.1.0
