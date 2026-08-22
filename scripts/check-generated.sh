#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
cleanup() {
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT

cd "$repo_root"

./scripts/fetch-specs.sh

go run ./cmd/mcpgen \
  -spec specs/mcp-tools.yaml \
  -out "$temp_dir/tools.gen.go"

go tool oapi-codegen \
  -generate types,client \
  -package tradeapi \
  -response-type-suffix HTTPResponse \
  -o "$temp_dir/trade.gen.go" \
  specs/trade.yaml

go tool oapi-codegen \
  -generate types,client \
  -package perpsapi \
  -response-type-suffix HTTPResponse \
  -o "$temp_dir/perps.gen.go" \
  specs/perps.yaml

cmp "$temp_dir/tools.gen.go" internal/gen/mcptools/tools.gen.go

echo "tracked MCP code is current; untracked upstream clients generate successfully"
