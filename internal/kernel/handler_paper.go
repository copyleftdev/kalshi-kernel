package kernel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/gen/mcptools"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/ledger"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/live"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/marketdata"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Paper cash default; override with KALSHI_PAPER_CASH_DOLLARS.
const defaultPaperCash = "100.00"

// paperLedger lazily builds the process-wide simulated book. Only call
// from paper-mode handlers.
func (handler *Handler) paperLedger() *ledger.Ledger {
	if handler.paper == nil {
		cash := os.Getenv("KALSHI_PAPER_CASH_DOLLARS")
		if cash == "" {
			cash = defaultPaperCash
		}
		l, err := ledger.New(cash)
		if err != nil {
			// Config validated at startup; a bad override falls back.
			l, _ = ledger.New(defaultPaperCash)
		}
		handler.paper = l
	}
	return handler.paper
}

// simulatedEnvelope marks every paper-trading response.
func simulatedEnvelope(data map[string]any) map[string]any {
	data["simulated"] = true
	data["warning"] = "paper fills do not predict live fills, liquidity, latency, slippage, fees, or profitability"
	return data
}

func (handler *Handler) GetPortfolio(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.GetPortfolioInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	if input.Product != "event" && input.Product != "perp" {
		return handler.respondError("get_portfolio", errors.New("product must be event or perp"))
	}
	if handler.config.Mode == config.ModeLive {
		return handler.livePortfolio(ctx)
	}
	cash, positions, journal := handler.paperLedger().Snapshot()
	data := map[string]any{
		"product":      input.Product,
		"cash_dollars": cash,
		"positions":    positions,
		"fills":        journal,
		"note":         "in-memory paper book; resets when the kernel process exits",
	}
	return handler.respond(simulatedEnvelope(data))
}

// liveClient lazily builds the process-wide authenticated account client.
// The private key is loaded from config once; it never leaves this struct.
func (handler *Handler) liveClient() (*live.Client, error) {
	if handler.live == nil {
		client, err := live.New(handler.config.APIKeyID, handler.config.PrivateKeyPath)
		if err != nil {
			return nil, err
		}
		handler.live = client
	}
	return handler.live, nil
}

// livePortfolio serves get_portfolio in live mode from the signed
// portfolio endpoints. Read-only: no order-write path exists here.
func (handler *Handler) livePortfolio(ctx context.Context) (*mcp.CallToolResult, mcptools.Response, error) {
	client, err := handler.liveClient()
	if err != nil {
		return handler.respondError("get_portfolio", err)
	}
	p, err := client.GetPortfolio(ctx)
	if err != nil {
		return handler.respondError("get_portfolio", err)
	}
	data := map[string]any{
		"product":   "event",
		"balance":   p.Balance,
		"positions": p.Positions,
		"orders":    p.Orders,
		"fills":     p.Fills,
		"simulated": false,
	}
	return handler.respond(data)
}

// bookTouch returns the top-of-book price/size for the requested side,
// plus a hash of the full snapshot used for the fill decision.
func bookTouch(ob *marketdata.Orderbook, side string) (price, size, hash string, err error) {
	raw, jerr := json.Marshal(ob)
	if jerr != nil {
		return "", "", "", jerr
	}
	sum := sha256.Sum256(raw)
	hash = hex.EncodeToString(sum[:])

	var levels []marketdata.PriceLevel
	if side == "bid" {
		// selling YES: cross at the yes bid
		levels = ob.Yes
	} else {
		// buying YES: cross at the yes ask == best no bid converted;
		// upstream lists no bids directly, use them: yes_ask = 1 - no_bid.
		levels = ob.No
	}
	if len(levels) == 0 || len(levels[0]) != 2 {
		return "", "", hash, errors.New("orderbook has no displayed liquidity at the touch")
	}
	price = levels[0][0]
	size = levels[0][1]
	return price, size, hash, nil
}

// yesAskFromNoBid converts the best no bid p into yes ask 1-p, keeping
// 4-decimal fixed-point exactness.
func yesAskFromNoBid(noBid string) (string, error) {
	v, err := strconv.ParseInt(noBidDigits(noBid), 10, 64)
	if err != nil {
		return "", err
	}
	ask := 10000 - v
	return fmt.Sprintf("%d.%04d", ask/10000, ask%10000), nil
}

func noBidDigits(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

func (handler *Handler) PlaceOrder(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.PlaceOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	if handler.config.Mode == config.ModeLive {
		return handler.livePlaceOrder(ctx, input)
	}
	if handler.config.Mode != "paper" {
		return handler.notReady("place_order")
	}
	if input.Side != "bid" && input.Side != "ask" {
		return handler.respondError("place_order", errors.New("side must be bid or ask"))
	}
	coid := input.Ticker + ":" + deref(input.ClientOrderID)
	if deref(input.ClientOrderID) == "" {
		coid = fmt.Sprintf("%s:auto:%s", input.Ticker, input.TimeInForce) // deterministic fallback
	}

	// Fetch a fresh orderbook snapshot; fills are priced only off it.
	var ob *marketdata.Orderbook
	var err error
	if input.Product == "event" {
		ob, err = handler.marketData().GetEventOrderbook(ctx, input.Ticker, 1)
	} else {
		ob, err = handler.marketData().GetMarginOrderbook(ctx, input.Ticker, 1)
	}
	if err != nil {
		return handler.respondError("place_order", err)
	}

	// Determine the executable touch.
	var touchPrice, touchSize, hash string
	if input.Side == "ask" {
		// sell YES: cross at the best (highest) yes bid == LAST level
		if len(ob.Yes) == 0 || len(ob.Yes[len(ob.Yes)-1]) != 2 {
			return handler.respondError("place_order", errors.New("orderbook has no displayed yes-bid liquidity"))
		}
		best := ob.Yes[len(ob.Yes)-1]
		touchPrice, touchSize = best[0], best[1]
		h := sha256.Sum256(mustJSON(ob))
		hash = hex.EncodeToString(h[:])
	} else {
		// buy YES at the yes ask = 1 - best no bid (LAST no level)
		if len(ob.No) == 0 || len(ob.No[len(ob.No)-1]) != 2 {
			return handler.respondError("place_order", errors.New("orderbook has no displayed no-bid liquidity"))
		}
		bestNo := ob.No[len(ob.No)-1]
		touchPrice, err = yesAskFromNoBid(bestNo[0])
		if err != nil {
			return handler.respondError("place_order", err)
		}
		touchSize = bestNo[1]
		h := sha256.Sum256(mustJSON(ob))
		hash = hex.EncodeToString(h[:])
	}
	if err != nil {
		return handler.respondError("place_order", err)
	}

	// The caller's price must equal the touch (v1: marketable limit only).
	if input.Price != touchPrice {
		return handler.respondError("place_order", fmt.Errorf(
			"price %s does not cross the current touch %s; paper v1 supports marketable orders only", input.Price, touchPrice))
	}

	res, lerr := handler.paperLedger().Execute(ledger.FillRequest{
		ClientOrderID: coid,
		Ticker:        input.Ticker,
		Side:          ledger.Side(input.Side),
		PriceDollars:  input.Price,
		CountFP:       input.Count,
		BookPrice:     touchPrice,
		BookSizeFP:    touchSize,
		BookHash:      hash,
	})
	if lerr != nil {
		return handler.respondTyped("place_order", lerr)
	}
	data := map[string]any{
		"product":        input.Product,
		"ticker":         input.Ticker,
		"side":           input.Side,
		"fill":           res.Fill,
		"replayed":       res.Replayed,
		"cash_after":     res.CashAfter,
		"orderbook_hash": hash,
	}
	return handler.respond(simulatedEnvelope(data))
}

func (handler *Handler) AmendOrder(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.AmendOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	// Paper v1 fills immediately all-or-nothing, so there is nothing to
	// amend. Keep the tool present with a precise typed failure.
	if handler.config.Mode == config.ModeLive {
		return handler.liveAmendOrder(ctx, input)
	}
	if handler.config.Mode != "paper" {
		return handler.notReady("amend_order")
	}
	return handler.respondError("amend_order", errors.New(
		"paper v1 fills are immediate all-or-nothing; there is no resting order to amend"))
}

func (handler *Handler) CancelOrder(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.CancelOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	if handler.config.Mode == config.ModeLive {
		return handler.liveCancelOrder(ctx, input)
	}
	if handler.config.Mode != "paper" {
		return handler.notReady("cancel_order")
	}
	return handler.respondError("cancel_order", ledger.ErrNoRestingOrder)
}

// liveCancelOrder cancels one resting event-market order through the
// signed live client. Stage 2: this is the only live write path.
func (handler *Handler) liveCancelOrder(
	ctx context.Context,
	input mcptools.CancelOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	if input.Product != "event" {
		return handler.respondError("cancel_order", errors.New("live cancel currently supports event-contract orders only"))
	}
	client, err := handler.liveClient()
	if err != nil {
		return handler.respondError("cancel_order", err)
	}
	res, err := client.CancelOrder(ctx, input.OrderID, input.Ticker)
	if err != nil {
		return &mcp.CallToolResult{IsError: true}, mcptools.Response{
			Mode:  string(handler.config.Mode),
			OK:    false,
			Error: &mcptools.Error{Code: live.Code(err), Message: "cancel_order: " + err.Error()},
		}, nil
	}
	return handler.respond(map[string]any{
		"product":         input.Product,
		"order_id":        res.OrderID,
		"client_order_id": res.ClientOrderID,
		"reduced_by_fp":   res.ReducedByFP,
		"ts_ms":           res.TsMs,
		"simulated":       false,
	})
}

// respondTyped maps a ledger sentinel error to its typed code.
func (handler *Handler) respondTyped(tool string, err error) (*mcp.CallToolResult, mcptools.Response, error) {
	code := "paper_rejected"
	switch {
	case errors.Is(err, ledger.ErrInsufficientBook):
		code = "insufficient_book"
	case errors.Is(err, ledger.ErrDuplicateOrder):
		code = "duplicate_client_order_id"
	case errors.Is(err, ledger.ErrNoRestingOrder):
		code = "no_resting_order"
	case errors.Is(err, ledger.ErrJournalFull):
		code = "journal_full"
	case errors.Is(err, ledger.ErrInsufficientCash):
		code = "insufficient_paper_cash"
	case errors.Is(err, ledger.ErrInvalidInput):
		code = "invalid_input"
	}
	return &mcp.CallToolResult{IsError: true}, mcptools.Response{
		Mode:  string(handler.config.Mode),
		OK:    false,
		Error: &mcptools.Error{Code: code, Message: tool + ": " + err.Error()},
	}, nil
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
