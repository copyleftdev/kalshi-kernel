# syntax=docker/dockerfile:1.7

FROM golang:1.25.13-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/kalshi-kernel \
    ./cmd/kalshi-kernel

FROM alpine:3.22 AS certificates

FROM scratch
LABEL org.opencontainers.image.title="Kalshi Kernel" \
      org.opencontainers.image.description="Unofficial safety-focused MCP server for Kalshi interoperability" \
      org.opencontainers.image.source="https://github.com/copyleftdev/kalshi-kernel" \
      org.opencontainers.image.licenses="Apache-2.0" \
      io.modelcontextprotocol.server.name="io.github.copyleftdev/kalshi-kernel"
COPY --from=certificates /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=build /out/kalshi-kernel /kalshi-kernel
COPY LICENSE NOTICE THIRD_PARTY_NOTICES.md /licenses/
USER 65532:65532
ENTRYPOINT ["/kalshi-kernel"]
