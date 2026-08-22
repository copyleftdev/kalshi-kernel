.PHONY: fetch-specs generate check check-generated test test-race test-fuzz test-alignment test-all conformance release-test public-check upstream-test upstream-check upstream-promote build

fetch-specs:
	./scripts/fetch-specs.sh

generate: fetch-specs
	go generate ./...

check: check-generated
	go test ./...
	go vet ./...

check-generated:
	./scripts/check-generated.sh

test: fetch-specs
	go test ./...

test-race: fetch-specs
	go test -race ./...

test-fuzz:
	go test -run '^$$' -fuzz '^FuzzLoadNeverEnablesUnexpectedMode$$' -fuzztime=10s ./internal/config

test-alignment: fetch-specs
	go test ./internal/specaudit

test-all:
	./scripts/test-all.sh

conformance:
	./scripts/conformance.sh

release-test: test-all conformance

public-check:
	./scripts/public-check.sh

upstream-test:
	./scripts/test-upstream.sh

upstream-check:
	./scripts/test-upstream.sh --require-current --conformance

upstream-promote:
	./scripts/test-upstream.sh --promote

build:
	go build -buildvcs=false -o bin/kalshi-kernel ./cmd/kalshi-kernel
