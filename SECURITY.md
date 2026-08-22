# Security policy

## Supported versions

Kalshi Kernel is pre-release software. Security fixes are applied only to the
latest version on the default branch unless a release announcement states
otherwise. No current version is approved for live trading.

## Report a vulnerability privately

Use GitHub's **Report a vulnerability** flow in the repository Security tab to
open a private security advisory. Do not create a public issue for a suspected
vulnerability.

If private vulnerability reporting has not yet been enabled, do not disclose
the details publicly. Ask the repository owner to enable it using a minimal
public issue that contains no exploit, credential, account, or user data.

Include, when possible:

- affected version or commit;
- impact and realistic attack scenario;
- reproduction steps or a minimal proof of concept;
- whether credentials, private keys, orders, balances, or positions are exposed;
- suggested remediation; and
- whether disclosure is time-sensitive.

Never include real Kalshi credentials, private keys, account identifiers, or
production trading data. Revoke or rotate any credential that may have been
exposed.

## Priority issues

The following are treated as security-sensitive:

- bypassing paper/live separation or the live acknowledgement;
- executing or reporting an order without explicit authorization;
- retry or idempotency behavior that can duplicate a trade;
- signature, credential, or private-key disclosure;
- tool-schema or prompt-injection paths that alter trade intent;
- accepting untrusted or redirected upstream specifications;
- remote authentication or authorization bypass;
- cross-user data exposure;
- unsafe logging of tool arguments or results; and
- supply-chain compromise of generated code or release artifacts.

## Maintainer response targets

Targets are best efforts, not guarantees:

- acknowledge a complete report within 3 business days;
- provide an initial severity assessment within 7 business days; and
- coordinate a fix and disclosure timeline appropriate to impact.

Public disclosure should occur only after a fix is available or an agreed
embargo expires.

## Operational guidance

- Default to paper mode.
- Use a dedicated, least-privilege account or subaccount.
- Keep private keys outside the repository and container image.
- Use secret managers and restrictive filesystem permissions.
- Apply exchange-side position, order, and loss limits independently.
- Treat MCP clients, models, prompts, upstream content, and tool input as
  untrusted.
- Monitor and reconcile every live action against the exchange.
- Maintain an out-of-band way to disable credentials and cancel orders.
