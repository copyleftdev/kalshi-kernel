#!/usr/bin/env bash
set -euo pipefail

readonly spec_files=(trade.yaml market_data_ws.yaml perps.yaml perps_ws.yaml)

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
temp_dir="$(mktemp -d)"
cleanup() {
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT

cd "$repo_root"
go run ./cmd/specfetch -output-dir "$temp_dir"
for filename in "${spec_files[@]}"; do
  install -m 0644 "$temp_dir/$filename" "$repo_root/specs/$filename"
done

echo "fetched validated upstream contracts into the ignored local specs cache"
