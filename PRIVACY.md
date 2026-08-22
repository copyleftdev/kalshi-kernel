# Privacy notice

Last updated: August 22, 2026

This notice describes the behavior of the open-source Kalshi Kernel software in
this repository. It does not cover Kalshi, an MCP client, a model provider, an
operating system, a deployment platform, or a third-party distribution service.
Those parties have their own terms and privacy notices.

## Current local release

Version `0.1.0` is a local stdio scaffold. The project does not operate a hosted
service and does not include analytics, telemetry, advertising, user tracking,
or maintainer-controlled data collection. Only `kernel_status` is operational;
the current release does not send market, portfolio, or order requests to
Kalshi.

The process reads configuration from its environment. In live mode it reads a
Kalshi API key identifier and a local private-key path. Paper mode discards
those values. The software does not intentionally print credential values or
include them in MCP tool metadata or results.

Your MCP client, shell, process supervisor, logs, crash reporter, container
platform, or operating system may still retain configuration, prompts, tool
arguments, and results. Configure those systems according to your own privacy
and retention requirements.

## Future adapters and hosted service

Market-data, portfolio, paper-trading, live-trading, and public remote-service
adapters are not implemented. Before any of them is released, this notice must
be updated to state accurately:

- what account, market, order, position, prompt, diagnostic, and device data is
  processed;
- the purpose and legal basis for processing;
- where processing and storage occur;
- retention and deletion periods;
- subprocessors and third-party sharing;
- security controls and incident notification practices;
- user access, correction, export, and deletion choices; and
- a monitored privacy contact and publisher identity.

A hosted deployment must publish its own HTTPS privacy notice. This repository
document is not sufficient for an operator whose deployment changes these data
flows.

## Third parties

When future adapters communicate with Kalshi, relevant data will be transmitted
to Kalshi under the user's account and Kalshi's policies. MCP clients and model
providers may process prompts, tool arguments, and results. Users should review
each provider's terms before connecting the server.

## Contact

Until a publisher identity and privacy contact are established, use the public
repository's issue tracker only for non-sensitive questions. Do not post
credentials, private keys, personal data, account data, or security reports in
a public issue. Security reports must follow [SECURITY.md](SECURITY.md).
