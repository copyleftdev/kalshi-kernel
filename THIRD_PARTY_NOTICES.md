# Third-party notices

Kalshi Kernel is an independent implementation that interoperates with public
interfaces maintained by third parties.

## Kalshi API contracts

Files downloaded from `https://docs.kalshi.com` include:

- `specs/trade.yaml` from `openapi.yaml`;
- `specs/market_data_ws.yaml` from `asyncapi.yaml`;
- `specs/perps.yaml` from `perps_openapi.yaml`; and
- `specs/perps_ws.yaml` from `perps_asyncapi.yaml`.

The AsyncAPI documents identify their license as “Proprietary” and link to
Kalshi's terms. The REST documents do not contain an affirmative open-source
license. These contracts, their contents, and Kalshi trademarks are not covered
by this repository's Apache License 2.0. No ownership or license grant from
Kalshi is claimed or implied.

The contract bodies are therefore fetched on demand into ignored local paths
and are not part of the Git release payload. Public releases must continue to
exclude them. REST clients generated from those contracts are likewise ignored
and regenerated locally. Maintainers must complete a rights review before any
future source or binary release includes those clients. The tracked provenance
lock records source URLs and digests but does not reproduce the contracts.

## Go dependencies

Go module dependencies and their versions are recorded in `go.mod` and
`go.sum`. Each dependency remains under its own license. Release automation
should produce a software bill of materials and retain dependency notices.
