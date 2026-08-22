package kernel

import (
	"context"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/gen/mcptools"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/ledger"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/live"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/marketdata"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Handler implements the curated MCP tool surface. Write-path tools remain
// fail-closed stubs until their execution adapters are connected; the three
// read-only market-data tools are wired to public REST endpoints.
type Handler struct {
	config config.Config

	// mdClient is a lazily-initialized read-only market-data client.
	// It holds no credentials and performs no retries.
	mdClient *marketdata.Client

	// paper is the lazily-initialized simulated book (paper mode only).
	paper *ledger.Ledger

	// live is the lazily-initialized signed account client (live mode
	// only). Stage 1: portfolio reads; no write path exists.
	live *live.Client
}

func New(config config.Config) *Handler {
	return &Handler{config: config}
}

// NewWithMarketData allows tests to inject a fixture-backed market-data
// client. Production callers should use New.
func NewWithMarketData(config config.Config, client *marketdata.Client) *Handler {
	return &Handler{config: config, mdClient: client}
}

func (handler *Handler) KernelStatus(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.KernelStatusInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return nil, mcptools.Response{
		Mode: string(handler.config.Mode),
		OK:   true,
		Data: map[string]any{
			"backend_ready":       false,
			"market_data_ready":   true,
			"paper_trading_ready": handler.config.Mode == config.ModePaper,
			"live_mode_requested": handler.config.Mode == config.ModeLive,
			"live_trading_armed":  false,
			"generated_from_spec": true,
			"reason":              "read-only market data and paper trading are live; live execution adapters are not connected",
		},
	}, nil
}

func (handler *Handler) notReady(tool string) (*mcp.CallToolResult, mcptools.Response, error) {
	return &mcp.CallToolResult{IsError: true}, mcptools.Response{
		Mode: string(handler.config.Mode),
		OK:   false,
		Error: &mcptools.Error{
			Code:    "capability_not_ready",
			Message: tool + " is specified but its execution adapter has not been connected",
		},
	}, nil
}
