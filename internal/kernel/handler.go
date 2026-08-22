package kernel

import (
	"context"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/gen/mcptools"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type Handler struct {
	config config.Config
}

func New(config config.Config) *Handler {
	return &Handler{config: config}
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
			"market_data_ready":   false,
			"live_mode_requested": handler.config.Mode == config.ModeLive,
			"live_trading_armed":  false,
			"generated_from_spec": true,
			"reason":              "execution adapters are not connected yet",
		},
	}, nil
}

func (handler *Handler) SearchMarkets(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.SearchMarketsInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return handler.notReady("search_markets")
}

func (handler *Handler) GetMarket(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.GetMarketInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return handler.notReady("get_market")
}

func (handler *Handler) GetOrderbook(
	context.Context,
	*mcp.CallToolRequest,
	mcptools.GetOrderbookInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	return handler.notReady("get_orderbook")
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
