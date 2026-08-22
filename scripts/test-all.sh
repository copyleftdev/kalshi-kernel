#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

./scripts/check-generated.sh
go test ./...
go test -race ./...
go test -run '^$' -fuzz '^FuzzLoadNeverEnablesUnexpectedMode$' -fuzztime=5s ./internal/config
go vet ./...
go build -buildvcs=false -o /tmp/kalshi-kernel-test-binary ./cmd/kalshi-kernel
