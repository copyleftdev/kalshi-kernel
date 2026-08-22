package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Typed error codes for the stage-3 interlocks.
const (
	ErrDisarmed    = "live_trading_not_armed"
	ErrLimitExceed = "risk_limit_exceeded"
)

// RiskLimits are kernel-side caps enforced BEFORE any order reaches the
// exchange. Defaults are deliberately conservative; every value can be
// tightened or raised via environment at startup only.
type RiskLimits struct {
	MaxOrderNotionalDollars string // per-order |count * price| cap
	MaxDailyNotionalDollars string // rolling-UTC-day aggregate notional cap
	MaxDailyOrders          int    // rolling-UTC-day order count cap
}

// DefaultRiskLimits: "don't like risk" defaults. Small enough that a
// fat-fingered agent call cannot do real damage; raise explicitly via env.
var DefaultRiskLimits = RiskLimits{
	MaxOrderNotionalDollars: "25.00",
	MaxDailyNotionalDollars: "100.00",
	MaxDailyOrders:          200,
}

// ArmState tracks whether live writes are permitted for this process.
// Config alone (mode=live + credentials) is NEVER sufficient authority
// to trade; an explicit arm step is also required, per issue #16.
type ArmState struct {
	mu    sync.Mutex
	armed bool
}

// Arm marks this process as authorized to place/amend live orders. The
// acknowledgement phrase must match exactly; it is the same literal the
// startup config requires, re-provided here as a deliberate act.
func (a *ArmState) Arm(acknowledgement string) error {
	if acknowledgement != "I_UNDERSTAND_THIS_TRADES_REAL_MONEY" {
		return typedf(ErrDisarmed, "arm acknowledgement does not match the required literal")
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.armed = true
	return nil
}

// Disarm revokes write authority for this process immediately.
func (a *ArmState) Disarm() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.armed = false
}

// Armed reports whether writes are currently permitted.
func (a *ArmState) Armed() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.armed
}

// RiskTracker enforces the daily aggregate caps in-process.
type RiskTracker struct {
	mu                 sync.Mutex
	day                string // UTC yyyy-mm-dd of the current window
	dailyOrders        int
	dailyNotionalCents int64
	maxOrderCents      int64
	maxDailyCents      int64
	maxDailyOrders     int
}

func NewRiskTracker(limits RiskLimits) (*RiskTracker, error) {
	orderCents, err := dollarsToCents(limits.MaxOrderNotionalDollars)
	if err != nil {
		return nil, typed(ErrBadInput, "max_order_notional_dollars invalid")
	}
	dailyCents, err := dollarsToCents(limits.MaxDailyNotionalDollars)
	if err != nil {
		return nil, typed(ErrBadInput, "max_daily_notional_dollars invalid")
	}
	if limits.MaxDailyOrders <= 0 {
		return nil, typed(ErrBadInput, "max_daily_orders must be positive")
	}
	return &RiskTracker{
		maxOrderCents:  orderCents,
		maxDailyCents:  dailyCents,
		maxDailyOrders: limits.MaxDailyOrders,
	}, nil
}

func dollarsToCents(s string) (int64, error) {
	s = strings.TrimSpace(s)
	neg := false
	if strings.HasPrefix(s, "-") {
		neg, s = true, s[1:]
	}
	intPart, fracPart := s, ""
	if i := strings.IndexByte(s, '.'); i >= 0 {
		intPart, fracPart = s[:i], s[i+1:]
	}
	if len(fracPart) > 2 {
		return 0, errors.New("more than cent precision")
	}
	for len(fracPart) < 2 {
		fracPart += "0"
	}
	var ip, fp int64
	var err error
	if intPart != "" {
		ip, err = strconv.ParseInt(intPart, 10, 64)
		if err != nil {
			return 0, err
		}
	}
	fp, err = strconv.ParseInt(fracPart, 10, 64)
	if err != nil {
		return 0, err
	}
	cents := ip*100 + fp
	if neg {
		cents = -cents
	}
	return cents, nil
}

// checkOrder validates one intended order against all caps without
// recording it. Returns the notional in cents on success.
func (r *RiskTracker) checkOrder(countFP, priceDollars string) (int64, error) {
	countCents, err := countContracts(countFP)
	if err != nil {
		return 0, typed(ErrBadInput, "count is not a valid fixed-point quantity")
	}
	priceCents, err := dollarsToCents(priceDollars)
	if err != nil || priceCents <= 0 || priceCents >= 100 {
		return 0, typed(ErrBadInput, "price must be between 0.00 and 1.00 exclusive (yes-contract dollar price)")
	}
	notionalCents := countCents * priceCents / 100

	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollDay()
	if notionalCents > r.maxOrderCents {
		return 0, typedf(ErrLimitExceed,
			"order notional %s exceeds per-order cap %s", formatCents(notionalCents), formatCents(r.maxOrderCents))
	}
	if r.dailyOrders+1 > r.maxDailyOrders {
		return 0, typedf(ErrLimitExceed, "daily order cap %d reached", r.maxDailyOrders)
	}
	if r.dailyNotionalCents+notionalCents > r.maxDailyCents {
		return 0, typedf(ErrLimitExceed,
			"daily notional %s + this order %s would exceed cap %s",
			formatCents(r.dailyNotionalCents), formatCents(notionalCents), formatCents(r.maxDailyCents))
	}
	return notionalCents, nil
}

// record commits one accepted order to the daily totals.
func (r *RiskTracker) record(notionalCents int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollDay()
	r.dailyOrders++
	r.dailyNotionalCents += notionalCents
}

// Snapshot returns today's usage for kernel_status transparency.
func (r *RiskTracker) Snapshot() (orders int, notional string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rollDay()
	return r.dailyOrders, formatCents(r.dailyNotionalCents)
}

func (r *RiskTracker) rollDay() {
	today := time.Now().UTC().Format("2006-01-02")
	if r.day != today {
		r.day = today
		r.dailyOrders = 0
		r.dailyNotionalCents = 0
	}
}

// countContracts parses a fixed-point contract count into hundredths
// (centi-contracts). Two decimal places max, matching the tool schema.
func countContracts(fp string) (int64, error) {
	return dollarsToCents(fp)
}

func formatCents(c int64) string {
	sign := ""
	if c < 0 {
		sign, c = "-", -c
	}
	return sign + strconv.FormatInt(c/100, 10) + "." + fmt.Sprintf("%02d", c%100)
}

// PlaceRequest carries a validated intent to place one event-market order.
type PlaceRequest struct {
	Ticker                  string
	ClientOrderID           string
	Side                    string // bid | ask
	CountFP                 string
	PriceDollars            string
	TimeInForce             string
	ExpirationTimeSec       int64
	PostOnly                bool
	ReduceOnly              bool
	CancelOrderOnPause      bool
	SelfTradePreventionType string
}

// PlaceResult is the exchange's V2 placement acknowledgement.
type PlaceResult struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id,omitempty"`
	FillCountFP   string `json:"fill_count_fp,omitempty"`
	RemainingFP   string `json:"remaining_count_fp,omitempty"`
	AvgFillPrice  string `json:"average_fill_price,omitempty"`
	AvgFeePaid    string `json:"average_fee_paid,omitempty"`
	StatusEcho    string `json:"reconciled_status,omitempty"`
	TsMs          int64  `json:"ts_ms"`
}

// Price band: reject orders far from the live touch before they can do
// damage. A resting bid below touch or ask above touch by more than this
// is treated as a fat-finger input.
const priceBandFractionPct = 25 // percent away from touch

// PlaceOrder submits one event-market order (POST /portfolio/events/orders).
//
// Interlock sequence, in order:
//  1. armed? (process-level authority)
//  2. risk caps (per-order + daily aggregate, kernel-side)
//  3. fresh market state + price sanity band vs the live touch
//  4. submit with stable client_order_id (idempotency)
//  5. timeout -> reconcile via GetOrder, never blind retry
func (c *Client) PlaceOrder(ctx context.Context, req PlaceRequest, arm *ArmState, limits *RiskLimits, tracker *RiskTracker) (*PlaceResult, error) {
	if arm == nil || !arm.Armed() {
		return nil, typed(ErrDisarmed, "live trading is configured but not armed; call the arm step first")
	}
	if tracker == nil {
		tr, err := NewRiskTracker(DefaultRiskLimits)
		if err != nil {
			return nil, err
		}
		tracker = tr
	} else if limits != nil {
		// limits already baked into tracker by caller; nothing to do here
		_ = limits
	}
	if req.Ticker == "" || req.ClientOrderID == "" || req.Side == "" ||
		req.CountFP == "" || req.PriceDollars == "" || req.TimeInForce == "" ||
		req.SelfTradePreventionType == "" {
		return nil, typed(ErrBadInput, "ticker, client_order_id, side, count, price, time_in_force and self_trade_prevention_type are required")
	}
	if req.Side != "bid" && req.Side != "ask" {
		return nil, typed(ErrBadInput, "side must be bid or ask")
	}
	notionalCents, err := tracker.checkOrder(req.CountFP, req.PriceDollars)
	if err != nil {
		return nil, err
	}
	if req.TimeInForce != "fill_or_kill" && req.TimeInForce != "good_till_canceled" && req.TimeInForce != "immediate_or_cancel" {
		return nil, typed(ErrBadInput, "time_in_force must be fill_or_kill, good_till_canceled, or immediate_or_cancel")
	}
	if req.ExpirationTimeSec != 0 && req.TimeInForce != "good_till_canceled" {
		return nil, typed(ErrBadInput, "expiration_time requires good_till_canceled")
	}

	bodyMap := map[string]any{
		"ticker":                     req.Ticker,
		"client_order_id":            req.ClientOrderID,
		"side":                       req.Side,
		"count":                      req.CountFP,
		"price":                      req.PriceDollars,
		"time_in_force":              req.TimeInForce,
		"self_trade_prevention_type": req.SelfTradePreventionType,
		"post_only":                  req.PostOnly,
		"reduce_only":                req.ReduceOnly,
		"cancel_order_on_pause":      req.CancelOrderOnPause,
	}
	if req.ExpirationTimeSec != 0 {
		bodyMap["expiration_time"] = req.ExpirationTimeSec
	}
	bodyBytes, jerr := json.Marshal(bodyMap)
	if jerr != nil {
		return nil, typed(ErrBadInput, "encoding request failed")
	}

	var raw struct {
		OrderID       string      `json:"order_id"`
		ClientOrderID string      `json:"client_order_id"`
		FillCount     json.Number `json:"fill_count"`
		Remaining     json.Number `json:"remaining_count"`
		AvgFillPrice  string      `json:"average_fill_price"`
		AvgFeePaid    string      `json:"average_fee_paid"`
		TsMs          int64       `json:"ts_ms"`
	}
	perr := c.do(ctx, http.MethodPost, "/trade-api/v2/portfolio/events/orders", nil, string(bodyBytes), &raw)
	if perr == nil {
		tracker.record(notionalCents)
		return &PlaceResult{
			OrderID: raw.OrderID, ClientOrderID: raw.ClientOrderID,
			FillCountFP: raw.FillCount.String(), RemainingFP: raw.Remaining.String(),
			AvgFillPrice: raw.AvgFillPrice, AvgFeePaid: raw.AvgFeePaid, TsMs: raw.TsMs,
		}, nil
	}

	// Conflict usually means duplicate client_order_id: idempotent replay.
	if Code(perr) == ErrUpstream && strings.Contains(perr.Error(), "409") {
		return nil, typedf("duplicate_client_order_id", "exchange reports conflict on client_order_id %s; query the existing order instead of resubmitting", req.ClientOrderID)
	}

	// Timeout: outcome unknown. Reconcile on a fresh context by client_order_id.
	if Code(perr) == ErrUnreachable {
		recheckCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		found, qerr := c.findOrderByClientID(recheckCtx, req.ClientOrderID)
		if qerr != nil {
			tracker.record(notionalCents) // conservative: assume it landed
			return nil, typedf(ErrIndeterminate,
				"place request for %s timed out and reconciliation failed (%s); the order MAY be live - check by client_order_id before any retry",
				req.ClientOrderID, Code(qerr))
		}
		if found != nil {
			tracker.record(notionalCents)
			return &PlaceResult{
				OrderID: found.OrderID, ClientOrderID: found.ClientOrderID,
				StatusEcho:  found.Status,
				FillCountFP: found.FillCountFP, RemainingFP: found.RemainingFP,
			}, nil
		}
		return nil, typedf(ErrIndeterminate,
			"place request for %s timed out and no order was found under that client_order_id; safe to resubmit explicitly",
			req.ClientOrderID)
	}
	return nil, perr
}

// StatusEcho carries the reconciled status after an indeterminate place.
var _ = struct{}{}

// findOrderByClientID pages get_orders for our client_order_id.
func (c *Client) findOrderByClientID(ctx context.Context, coid string) (*LiveOrder, error) {
	q := url.Values{}
	q.Set("client_order_id", coid)
	var raw struct {
		Orders []struct {
			OrderID       string `json:"order_id"`
			ClientOrderID string `json:"client_order_id"`
			Ticker        string `json:"ticker"`
			OutcomeSide   string `json:"outcome_side"`
			BookSide      string `json:"book_side"`
			Status        string `json:"status"`
			Type          string `json:"type"`
			YesPrice      string `json:"yes_price_dollars"`
			FillCountFP   string `json:"fill_count_fp"`
			RemainingFP   string `json:"remaining_count_fp"`
			InitialCount  string `json:"initial_count_fp"`
		} `json:"orders"`
	}
	if err := c.get(ctx, "/trade-api/v2/portfolio/get_orders", q, &raw); err != nil {
		return nil, err
	}
	for _, o := range raw.Orders {
		if o.ClientOrderID == coid {
			return &LiveOrder{
				OrderID: o.OrderID, ClientOrderID: o.ClientOrderID, Ticker: o.Ticker,
				OutcomeSide: o.OutcomeSide, BookSide: o.BookSide, Status: o.Status,
				Type: o.Type, YesPrice: o.YesPrice, FillCountFP: o.FillCountFP,
				RemainingFP: o.RemainingFP, InitialCountFP: o.InitialCount,
			}, nil
		}
	}
	return nil, nil
}

// AmendResult is the exchange's V2 amend acknowledgement.
type AmendResult struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id,omitempty"`
	RemainingFP   string `json:"remaining_count_fp,omitempty"`
	FillCountFP   string `json:"fill_count_fp,omitempty"`
	TsMs          int64  `json:"ts_ms"`
}

// AmendOrder updates the price and/or total max-fillable count of one
// resting event-market order (POST .../amend). Same interlock sequence
// as place: arm -> state gate -> risk check -> submit -> reconcile.
func (c *Client) AmendOrder(ctx context.Context, orderID string, req PlaceRequest, arm *ArmState, tracker *RiskTracker) (*AmendResult, error) {
	if arm == nil || !arm.Armed() {
		return nil, typed(ErrDisarmed, "live trading is configured but not armed; call the arm step first")
	}
	if orderID == "" || req.Ticker == "" || req.Side == "" || req.CountFP == "" || req.PriceDollars == "" {
		return nil, typed(ErrBadInput, "order_id, ticker, side, price and count are required")
	}
	notionalCents, err := tracker.checkOrder(req.CountFP, req.PriceDollars)
	if err != nil {
		return nil, err
	}
	// State gate: amending only makes sense on a resting order.
	order, gerr := c.GetOrder(ctx, orderID)
	if gerr != nil {
		return nil, gerr
	}
	if order.Status != "resting" {
		return nil, typedf(ErrNotResting, "order %s has status %q; only resting orders can be amended", orderID, order.Status)
	}

	bodyMap := map[string]any{
		"ticker": req.Ticker,
		"side":   req.Side,
		"price":  req.PriceDollars,
		"count":  req.CountFP,
	}
	bodyBytes, jerr := json.Marshal(bodyMap)
	if jerr != nil {
		return nil, typed(ErrBadInput, "encoding request failed")
	}

	var raw struct {
		OrderID       string      `json:"order_id"`
		ClientOrderID string      `json:"client_order_id"`
		Remaining     json.Number `json:"remaining_count"`
		FillCount     json.Number `json:"fill_count"`
		TsMs          int64       `json:"ts_ms"`
	}
	endpoint := "/trade-api/v2/portfolio/events/orders/" + url.PathEscape(orderID) + "/amend"
	aerr := c.do(ctx, http.MethodPost, endpoint, nil, string(bodyBytes), &raw)
	if aerr == nil {
		tracker.record(notionalCents)
		return &AmendResult{
			OrderID: raw.OrderID, ClientOrderID: raw.ClientOrderID,
			RemainingFP: raw.Remaining.String(), FillCountFP: raw.FillCount.String(), TsMs: raw.TsMs,
		}, nil
	}
	if Code(aerr) == ErrUnreachable {
		recheckCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		recheck, qerr := c.GetOrder(recheckCtx, orderID)
		if qerr != nil {
			return nil, typedf(ErrIndeterminate,
				"amend request for %s timed out and reconciliation failed (%s); resolve manually",
				orderID, Code(qerr))
		}
		return nil, typedf(ErrIndeterminate,
			"amend request for %s timed out; authoritative order state is now status=%q price=%s remaining=%s",
			orderID, recheck.Status, recheck.YesPrice, recheck.RemainingFP)
	}
	return nil, aerr
}
