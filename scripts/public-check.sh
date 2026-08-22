#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

go mod verify
go tool govulncheck ./...
./scripts/check-generated.sh
go test ./...
go vet ./...
go build -buildvcs=false -trimpath -o /tmp/kalshi-kernel-public-check ./cmd/kalshi-kernel
KALSHI_KERNEL_MODE=paper /tmp/kalshi-kernel-public-check </dev/null

echo "public repository checks passed"
