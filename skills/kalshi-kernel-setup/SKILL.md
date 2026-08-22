---
name: kalshi-kernel-setup
description: Set up, verify, or troubleshoot the local Kalshi Kernel MCP server in paper mode. Use when the user asks to install or connect Kalshi Kernel.
---

# Set up Kalshi Kernel

Kalshi Kernel is unofficial pre-release software. State clearly that version
0.1.0 has no connected market, paper-ledger, portfolio, or live-trading adapter.
Do not imply affiliation with Kalshi or readiness for trading.

1. Confirm the user trusts the public repository and has reviewed `DISCLAIMER.md`.
2. Prefer a signed/checksummed GitHub release binary once releases exist. Until
   then, build from source with Go 1.25.13+ using `make build`.
3. Put the resulting `kalshi-kernel` executable on `PATH`; never download or run
   an unverified binary silently.
4. Keep `KALSHI_KERNEL_MODE=paper`. Do not request API credentials or a private
   key for the current scaffold.
5. Enable the plugin and inspect `/mcp`, or add the server directly with:

   ```sh
   claude mcp add --scope user --transport stdio kalshi-kernel \
     --env KALSHI_KERNEL_MODE=paper -- /absolute/path/to/kalshi-kernel
   ```

6. Call `kernel_status`. A correct 0.1.0 setup reports paper mode,
   `backend_ready: false`, and `market_data_ready: false`.
7. If another tool returns `capability_not_ready`, explain that this is expected
   fail-closed behavior, not a successful market or order operation.

Never ask the user to paste credentials, private keys, account data, or trading
history into chat or an issue.
