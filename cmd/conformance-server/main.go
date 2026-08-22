// Command conformance-server exposes the production MCP server over a
// loopback-only HTTP transport for the official MCP conformance runner.
// It is deliberately paper-only and is not a deployment entry point.
package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"time"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	address := flag.String("address", "127.0.0.1:8765", "loopback address used only by the conformance runner")
	stateless := flag.Bool("stateless", false, "serve the MCP 2026-07-28 stateless lifecycle")
	flag.Parse()

	host, _, err := net.SplitHostPort(*address)
	if err != nil {
		log.Fatalf("invalid address: %v", err)
	}
	ip := net.ParseIP(host)
	if host != "localhost" && (ip == nil || !ip.IsLoopback()) {
		log.Fatal("conformance server must bind to a loopback address")
	}

	server := mcpserver.New(config.Config{Mode: config.ModePaper})
	handler := mcp.NewStreamableHTTPHandler(
		func(*http.Request) *mcp.Server { return server },
		&mcp.StreamableHTTPOptions{Stateless: *stateless},
	)
	mux := http.NewServeMux()
	mux.Handle("/mcp", handler)

	httpServer := &http.Server{
		Addr:              *address,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	fmt.Printf("conformance MCP server listening at http://%s/mcp\n", *address)
	if err := httpServer.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}
