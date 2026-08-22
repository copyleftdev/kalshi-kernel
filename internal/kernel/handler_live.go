package kernel

import (
	"context"
	"errors"
	"os"
	"strconv"

	"github.com/copyleftdev/kalshi-kernel/internal/config"
	"github.com/copyleftdev/kalshi-kernel/internal/gen/mcptools"
	"github.com/copyleftdev/kalshi-kernel/internal/kernel/live"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// liveTracker lazily builds the kernel-side risk tracker from env-tunable
// conservative defaults:
//
//	KALSHI_MAX_ORDER_NOTIONAL_DOLLARS  (default 25.00)
//	KALSHI_MAX_DAILY_NOTIONAL_DOLLARS  (default 100.00)
//	KALSHI_MAX_DAILY_ORDERS            (default 200)
//
// Values can be tightened or raised at startup only; never via tool calls.
func (handler *Handler) liveTracker() (*live.RiskTracker, error) {
	if handler.tracker != nil {
		return handler.tracker, nil
	}
	limits := live.DefaultRiskLimits
	if v := os.Getenv("KALSHI_MAX_ORDER_NOTIONAL_DOLLARS"); v != "" {
		limits.MaxOrderNotionalDollars = v
	}
	if v := os.Getenv("KALSHI_MAX_DAILY_NOTIONAL_DOLLARS"); v != "" {
		limits.MaxDailyNotionalDollars = v
	}
	if v := os.Getenv("KALSHI_MAX_DAILY_ORDERS"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return nil, typedErr(live.ErrBadInput, "KALSHI_MAX_DAILY_ORDERS is not an integer")
		}
		limits.MaxDailyOrders = n
	}
	tracker, err := live.NewRiskTracker(limits)
	if err != nil {
		return nil, err
	}
	handler.tracker = tracker
	return handler.tracker, nil
}

// typedErr builds a live-package-compatible typed error.
func typedErr(code, message string) error { return &live.TypedError{Code: code, Message: message} }

// respondErrorLive maps a live-package error onto a typed failure.
func (handler *Handler) respondErrorLive(tool string, err error) (*mcp.CallToolResult, mcptools.Response, error) {
	return &mcp.CallToolResult{IsError: true}, mcptools.Response{
		Mode:  string(handler.config.Mode),
		OK:    false,
		Error: &mcptools.Error{Code: live.Code(err), Message: tool + ": " + err.Error()},
	}, nil
}

// ArmLiveTrading arms or disarms live order placement for this process.
// Arming requires repeating the exact startup acknowledgement as a
// deliberate second act; config alone is never authority to trade.
func (handler *Handler) ArmLiveTrading(
	ctx context.Context,
	req *mcp.CallToolRequest,
	input mcptools.ArmLiveTradingInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	if handler.config.Mode != config.ModeLive {
		return handler.respondError("arm_live_trading", errors.New("arming is only meaningful in live mode"))
	}
	var err error
	if input.Arm {
		err = handler.arm.Arm(input.Acknowledgement)
	} else {
		handler.arm.Disarm()
	}
	if err != nil {
		return handler.respondErrorLive("arm_live_trading", err)
	}
	data := map[string]any{
		"armed":     handler.arm.Armed(),
		"simulated": false,
	}
	if orders, notional, trerr := handler.trackerSnapshot(); trerr == nil {
		data["daily_orders_used"] = orders
		data["daily_notional_used_dollars"] = notional
	}
	return handler.respond(data)
}

// trackerSnapshot exposes daily usage; best-effort, never fatal.
func (handler *Handler) trackerSnapshot() (int, string, error) {
	tracker, err := handler.liveTracker()
	if err != nil {
		return 0, "", err
	}
	orders, notional := tracker.Snapshot()
	return orders, notional, nil
}

// livePlaceOrder routes place_order to the signed live client under the
// full stage-3 interlock sequence: arm -> risk caps -> submit -> reconcile.
func (handler *Handler) livePlaceOrder(
	ctx context.Context,
	input mcptools.PlaceOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	if input.Product != "event" {
		return handler.respondError("place_order", errors.New("live placement currently supports event-contract markets only"))
	}
	if input.Subaccount != nil && *input.Subaccount != 0 {
		return handler.respondError("place_order", errors.New("subaccounts are not supported yet; omit or pass 0"))
	}
	// Arm gate BEFORE any credential use: an un-armed process must refuse
	// writes without even loading key material.
	if !handler.arm.Armed() {
		return handler.respondErrorLive("place_order", typedErr(live.ErrDisarmed, "live trading is configured but not armed; call arm_live_trading first"))
	}
	client, err := handler.liveClient()
	if err != nil {
		return handler.respondError("place_order", err)
	}
	tracker, err := handler.liveTracker()
	if err != nil {
		return handler.respondError("place_order", err)
	}
	preq := live.PlaceRequest{
		Ticker:                  input.Ticker,
		ClientOrderID:           deref(input.ClientOrderID),
		Side:                    input.Side,
		CountFP:                 input.Count,
		PriceDollars:            input.Price,
		TimeInForce:             input.TimeInForce,
		ExpirationTimeSec:       derefInt64(input.ExpirationTime),
		PostOnly:                derefBool(input.PostOnly),
		ReduceOnly:              derefBool(input.ReduceOnly),
		CancelOrderOnPause:      derefBool(input.CancelOrderOnPause),
		SelfTradePreventionType: input.SelfTradePreventionType,
	}
	if preq.ClientOrderID == "" {
		return handler.respondError("place_order", errors.New("client_order_id is required in live mode; generate a stable unique id per logical order"))
	}
	res, err := client.PlaceOrder(ctx, preq, &handler.arm, nil, tracker)
	if err != nil {
		return handler.liveWriteError("place_order", err)
	}
	return handler.respond(map[string]any{
		"product":            input.Product,
		"ticker":             input.Ticker,
		"order_id":           res.OrderID,
		"client_order_id":    res.ClientOrderID,
		"fill_count_fp":      res.FillCountFP,
		"remaining_count_fp": res.RemainingFP,
		"average_fill_price": res.AvgFillPrice,
		"average_fee_paid":   res.AvgFeePaid,
		"ts_ms":              res.TsMs,
		"simulated":          false,
	})
}

// liveAmendOrder routes amend_order to the signed live client with the
// same interlock sequence as place.
func (handler *Handler) liveAmendOrder(
	ctx context.Context,
	input mcptools.AmendOrderInput,
) (*mcp.CallToolResult, mcptools.Response, error) {
	if input.Product != "event" {
		return handler.respondError("amend_order", errors.New("live amend currently supports event-contract orders only"))
	}
	if input.Subaccount != nil && *input.Subaccount != 0 {
		return handler.respondError("amend_order", errors.New("subaccounts are not supported yet; omit or pass 0"))
	}
	if !handler.arm.Armed() {
		return handler.respondErrorLive("amend_order", typedErr(live.ErrDisarmed, "live trading is configured but not armed; call arm_live_trading first"))
	}
	client, err := handler.liveClient()
	if err != nil {
		return handler.respondError("amend_order", err)
	}
	tracker, err := handler.liveTracker()
	if err != nil {
		return handler.respondError("amend_order", err)
	}
	preq := live.PlaceRequest{
		Ticker:       input.Ticker,
		Side:         input.Side,
		CountFP:      input.Count,
		PriceDollars: input.Price,
	}
	res, err := client.AmendOrder(ctx, input.OrderID, preq, &handler.arm, tracker)
	if err != nil {
		return handler.liveWriteError("amend_order", err)
	}
	return handler.respond(map[string]any{
		"product":            input.Product,
		"order_id":           res.OrderID,
		"client_order_id":    res.ClientOrderID,
		"remaining_count_fp": res.RemainingFP,
		"fill_count_fp":      res.FillCountFP,
		"ts_ms":              res.TsMs,
		"simulated":          false,
	})
}

// liveWriteError maps live write failures onto typed codes.
func (handler *Handler) liveWriteError(tool string, err error) (*mcp.CallToolResult, mcptools.Response, error) {
	return &mcp.CallToolResult{IsError: true}, mcptools.Response{
		Mode:  string(handler.config.Mode),
		OK:    false,
		Error: &mcptools.Error{Code: live.Code(err), Message: tool + ": " + err.Error()},
	}, nil
}
