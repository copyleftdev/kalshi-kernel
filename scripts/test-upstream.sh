#!/usr/bin/env bash
set -euo pipefail

require_current=false
promote=false
run_conformance=false
for argument in "$@"; do
  case "$argument" in
    --require-current) require_current=true ;;
    --promote) promote=true; run_conformance=true ;;
    --conformance) run_conformance=true ;;
    *) echo "unknown argument: $argument" >&2; exit 2 ;;
  esac
done
if [[ "$require_current" == true && "$promote" == true ]]; then
  echo "--require-current and --promote are mutually exclusive" >&2
  exit 2
fi

readonly spec_files=(trade.yaml market_data_ws.yaml perps.yaml perps_ws.yaml)
readonly generated_files=(
  internal/gen/mcptools/tools.gen.go
)

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
workspace="$temp_dir/workspace"
cleanup() {
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT

cp -a "$repo_root/." "$workspace"
cd "$workspace"

go run ./cmd/specfetch -output-dir "$workspace/specs"

drift=false
for filename in "${spec_files[@]}"; do
  expected_digest="$(jq -r --arg filename "$filename" '.specs[] | select(.filename == $filename) | .sha256' "$repo_root/specs/upstream.lock.json")"
  upstream_digest="$(sha256sum "$workspace/specs/$filename" | cut -d ' ' -f 1)"
  if [[ -z "$expected_digest" || "$expected_digest" == "null" ]]; then
    echo "provenance lock has no digest for specs/$filename" >&2
    exit 1
  fi
  if [[ "$expected_digest" != "$upstream_digest" ]]; then
    drift=true
    echo "upstream drift: specs/$filename"
    echo "  locked:   $expected_digest"
    echo "  upstream: $upstream_digest"
  fi
done

go generate ./...
./scripts/check-generated.sh
go test ./...
go test -race ./...
go vet ./...
go build -buildvcs=false -o "$temp_dir/kalshi-kernel" ./cmd/kalshi-kernel

if [[ "$run_conformance" == true ]]; then
  ./scripts/conformance.sh
fi

if [[ "$promote" == true ]]; then
  install -m 0644 "$workspace/specs/upstream.lock.json" "$repo_root/specs/upstream.lock.json"
  for filename in "${generated_files[@]}"; do
    install -m 0644 "$workspace/$filename" "$repo_root/$filename"
  done
  echo "promoted the tested provenance lock and generated clients; contract bodies remain untracked"
  exit 0
fi

if [[ "$require_current" == true && "$drift" == true ]]; then
  echo "upstream specifications are compatible but the provenance lock is stale" >&2
  echo "run: make upstream-promote" >&2
  exit 1
fi

if [[ "$drift" == true ]]; then
  echo "latest upstream contracts built and passed; provenance lock differs"
else
  echo "latest upstream contracts built and passed; provenance lock is current"
fi
