package main

import (
	"context"
	"errors"
	"io"
	"log"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/mcpserver"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	config, err := config.Load()
	if err != nil {
		log.Fatal(err)
	}

	server := mcpserver.New(config)
	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil && !errors.Is(err, io.EOF) {
		log.Fatal(err)
	}
}
