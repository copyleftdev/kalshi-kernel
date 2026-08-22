package mcpserver

import (
	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/gen/mcptools"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func New(config config.Config) *mcp.Server {
	server := mcp.NewServer(&mcp.Implementation{
		Name:    mcptools.ServerName,
		Version: mcptools.ServerVersion,
	}, &mcp.ServerOptions{Instructions: "Unofficial, unaffiliated Kalshi integration. Call kernel_status before other tools and distinguish paper from live mode. Never report a market or order action as successful unless the tool returns ok=true. Live orders can cause financial loss and require explicit user authorization; this server does not provide financial advice."})
	mcptools.Register(server, kernel.New(config))
	return server
}
