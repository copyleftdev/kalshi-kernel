package kernel

import (
	"context"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/gen/mcptools"
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
			"live_mode_requested": handler.config.Mode == config.ModeLive,
			"live_trading_armed":  false,
			"generated_from_spec": true,
			"reason":              "read-only market data is live via public REST; execution adapters are not connected yet",
		},
	}, nil
}

func (handler *Handler) GetPortfolio(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.GetPortfolioInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return handler.notReady("get_portfolio")
}

func (handler *Handler) PlaceOrder(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.PlaceOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return handler.notReady("place_order")
}

func (handler *Handler) AmendOrder(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.AmendOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return handler.notReady("amend_order")
}

func (handler *Handler) CancelOrder(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.CancelOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return handler.notReady("cancel_order")
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
