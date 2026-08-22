package kernel

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/copyleftdev/kalshi-kernel/internal/gen/mcptools"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/marketdata"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// marketData returns the process-wide read-only client. Reads use public
// endpoints and never require or consult credentials; paper mode therefore
// keeps its "discards live credential configuration" invariant.
func (handler *Handler) marketData() *marketdata.Client {
	if handler.mdClient == nil {
		handler.mdClient = marketdata.NewClient()
	}
	return handler.mdClient
}

// respond wraps a successful typed response.
func (handler *Handler) respond(data map[string]any) (*mcp.CallToolResult, mcptools.Response, error) {
	return nil, mcptools.Response{
		Mode: string(handler.config.Mode),
		OK:   true,
		Data: data,
	}, nil
}

// respondError maps an adapter error onto a typed failure without
// performing any retry. The agent decides whether and when to retry.
func (handler *Handler) respondError(tool string, err error) (*mcp.CallToolResult, mcptools.Response, error) {
	return &mcp.CallToolResult{IsError: true}, mcptools.Response{
		Mode: string(handler.config.Mode),
		OK:   false,
		Error: &mcptools.Error{
			Code:    marketdata.Code(err),
			Message: tool + ": " + err.Error(),
		},
	}, nil
}

func (handler *Handler) SearchMarkets(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.SearchMarketsInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	client := handler.marketData()
	opt := marketdata.SearchOptions{
		Tickers:      input.Tickers,
		EventTicker:  deref(input.EventTicker),
		SeriesTicker: deref(input.SeriesTicker),
		Status:       deref(input.Status),
		Limit:        derefInt(input.Limit),
		Cursor:       deref(input.Cursor),
	}
	var (
		page *marketdata.MarketsPage
		err  error
	)
	switch input.Product {
	case "event":
		page, err = client.SearchEventMarkets(ctx, opt)
	case "perp":
		page, err = client.SearchMarginMarkets(ctx, opt)
	default:
		return handler.respondError("search_markets", errors.New("product must be event or perp"))
	}
	if err != nil {
		return handler.respondError("search_markets", err)
	}
	data := map[string]any{"product": input.Product}
	raw, _ := json.Marshal(page)
	var generic map[string]any
	_ = json.Unmarshal(raw, &generic)
	for k, v := range generic {
		data[k] = v
	}
	return handler.respond(data)
}

func (handler *Handler) GetMarket(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.GetMarketInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	client := handler.marketData()
	var (
		raw json.RawMessage
		err error
	)
	switch input.Product {
	case "event":
		raw, err = client.GetEventMarket(ctx, input.Ticker)
	case "perp":
		raw, err = client.GetMarginMarket(ctx, input.Ticker)
	default:
		return handler.respondError("get_market", errors.New("product must be event or perp"))
	}
	if err != nil {
		return handler.respondError("get_market", err)
	}
	var generic map[string]any
	if jerr := json.Unmarshal(raw, &generic); jerr != nil || generic == nil {
		generic = map[string]any{}
	}
	data := map[string]any{"product": input.Product, "ticker": input.Ticker, "market": generic}
	return handler.respond(data)
}

func (handler *Handler) GetOrderbook(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.GetOrderbookInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	client := handler.marketData()
	depth := derefInt(input.Depth)
	var (
		ob  *marketdata.Orderbook
		err error
	)
	switch input.Product {
	case "event":
		ob, err = client.GetEventOrderbook(ctx, input.Ticker, depth)
	case "perp":
		ob, err = client.GetMarginOrderbook(ctx, input.Ticker, depth)
	default:
		return handler.respondError("get_orderbook", errors.New("product must be event or perp"))
	}
	if err != nil {
		return handler.respondError("get_orderbook", err)
	}
	data := map[string]any{
		"product":   input.Product,
		"ticker":    input.Ticker,
		"orderbook": ob,
		"note":      "yes bids and no bids only; a bid for no at price p is equivalent to an ask for yes at 1-p",
	}
	return handler.respond(data)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func derefInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
