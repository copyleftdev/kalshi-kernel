#!/usr/bin/env bash
set -euo pipefail

readonly conformance_version="0.1.16"
readonly protocol_version="2025-11-25"
readonly address="127.0.0.1:8765"
readonly endpoint="http://${address}/mcp"
readonly scenarios=(
  server-initialize
  logging-set-level
  ping
  tools-list
  server-sse-multiple-streams
  resources-list
  prompts-list
  dns-rebinding-protection
)

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
server_pid=""
cleanup() {
  if [[ -n "$server_pid" ]]; then
    kill "$server_pid" 2>/dev/null || true
    wait "$server_pid" 2>/dev/null || true
  fi
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT

cd "$repo_root"
go build -buildvcs=false -o "$temp_dir/conformance-server" ./cmd/conformance-server
"$temp_dir/conformance-server" -address "$address" >"$temp_dir/server.log" 2>&1 &
server_pid="$!"

ready=false
for _ in {1..50}; do
  if curl -s -o /dev/null "$endpoint"; then
    ready=true
    break
  fi
  sleep 0.1
done
if [[ "$ready" != true ]]; then
  cat "$temp_dir/server.log" >&2
  echo "conformance server did not become ready" >&2
  exit 1
fi

for scenario in "${scenarios[@]}"; do
  npx -y "@modelcontextprotocol/conformance@${conformance_version}" server \
    --url "$endpoint" \
    --scenario "$scenario" \
    --spec-version "$protocol_version" \
    --output-dir "$temp_dir/results"
done

failed=false
checks_count=0
while IFS= read -r checks_file; do
  checks_count=$((checks_count + 1))
  if ! jq -e 'all(.[]; .status == "SUCCESS")' "$checks_file" >/dev/null; then
    echo "failed conformance checks in $checks_file" >&2
    jq '.[] | select(.status != "SUCCESS")' "$checks_file" >&2
    failed=true
  fi
done < <(find "$temp_dir/results" -name checks.json -type f -print)

if [[ "$checks_count" -ne "${#scenarios[@]}" ]]; then
  echo "expected ${#scenarios[@]} conformance result files, found $checks_count" >&2
  exit 1
fi

if [[ "$failed" == true ]]; then
  exit 1
fi

echo "official MCP conformance scenarios passed (${protocol_version})"
