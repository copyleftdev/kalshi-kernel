// Package live provides a signed, read-only Kalshi account client for
// live mode. Stage 1 of the execution adapter: portfolio reads only —
// no order writes are reachable through this package.
//
// Credential rules (docs/THREAT_MODEL.md):
//   - the private key is loaded once from disk at construction;
//   - key material never appears in MCP arguments, results, or errors;
//   - every failure is a typed error; nothing is retried here.
package live

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	defaultBaseURL  = "https://api.elections.kalshi.com"
	defaultTimeout  = 10 * time.Second
	maxResponseSize = 8 << 20
)

// Typed error codes surfaced through mcptools.Response.Error.Code.
const (
	ErrUpstream      = "upstream_error"
	ErrRateLimited   = "rate_limited"
	ErrUnauthed      = "not_authorized"
	ErrBadInput      = "invalid_input"
	ErrUnreachable   = "upstream_unreachable"
	ErrBadPayload    = "upstream_payload_invalid"
	ErrBadKey        = "credential_invalid"
	ErrNotFound      = "order_not_found"
	ErrNotResting    = "order_not_resting"
	ErrIndeterminate = "outcome_indeterminate"
)

// Client performs authenticated GET requests against Kalshi portfolio
// endpoints using RSA-PSS request signatures.
type Client struct {
	apiKeyID   string
	privateKey *rsa.PrivateKey
	baseURL    string
	http       *http.Client
}

// New loads the private key from keyPath and binds a client to the
// production Kalshi host. The key must be PKCS#1 or PKCS#8 PEM.
func New(apiKeyID, keyPath string) (*Client, error) {
	if apiKeyID == "" || keyPath == "" {
		return nil, &typedError{code: ErrBadInput, message: "api key id and private key path are required"}
	}
	pemBytes, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, &typedError{code: ErrBadKey, message: "reading private key failed"}
	}
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, &typedError{code: ErrBadKey, message: "private key is not PEM encoded"}
	}
	var key *rsa.PrivateKey
	if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		key = k
	} else if parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		rsaKey, ok := parsed.(*rsa.PrivateKey)
		if !ok {
			return nil, &typedError{code: ErrBadKey, message: "private key is not RSA"}
		}
		key = rsaKey
	} else {
		return nil, &typedError{code: ErrBadKey, message: "private key is not PKCS#1 or PKCS#8"}
	}
	return &Client{
		apiKeyID:   apiKeyID,
		privateKey: key,
		baseURL:    defaultBaseURL,
		http:       &http.Client{Timeout: defaultTimeout},
	}, nil
}

// NewWithBaseURL is used by tests to point the client at a fixture server.
func NewWithBaseURL(apiKeyID string, key *rsa.PrivateKey, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{apiKeyID: apiKeyID, privateKey: key, baseURL: baseURL, http: httpClient}
}

type typedError struct {
	code    string
	message string
}

func (e *typedError) Error() string { return e.code + ": " + e.message }

// Code maps an error to its typed code for mcptools.Response.Error.
func Code(err error) string {
	var te *typedError
	if errors.As(err, &te) {
		return te.code
	}
	return "internal_error"
}

func typed(code, message string) error { return &typedError{code: code, message: message} }

func typedf(code, format string, args ...any) error {
	return &typedError{code: code, message: fmt.Sprintf(format, args...)}
}

// sign produces the RSA-PSS signature over "timestamp + METHOD + path[?query]"
// exactly as the Kalshi API expects.
func (c *Client) sign(timestampMs int64, method, pathWithQuery string) (string, error) {
	base := strconv.FormatInt(timestampMs, 10) + method + pathWithQuery
	digest := sha256.Sum256([]byte(base))
	sig, err := rsa.SignPSS(rand.Reader, c.privateKey, crypto.SHA256, digest[:], &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash})
	if err != nil {
		return "", &typedError{code: ErrBadKey, message: "signing request failed"}
	}
	return base64.StdEncoding.EncodeToString(sig), nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values, out any) error {
	return c.do(ctx, http.MethodGet, path, query, out)
}

// do performs one signed request. The signature base uses the path with
// query string for GET and the bare path for body-carrying methods,
// matching the Kalshi signing contract.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, out any) error {
	full := path
	if len(query) > 0 {
		full += "?" + query.Encode()
	}
	ts := time.Now().UnixMilli()
	sig, err := c.sign(ts, method, full)
	if err != nil {
		return err
	}
	req, rerr := http.NewRequestWithContext(ctx, method, c.baseURL+full, nil)
	if rerr != nil {
		return typed(ErrBadInput, "request construction failed")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("KALSHI-ACCESS-KEY", c.apiKeyID)
	req.Header.Set("KALSHI-ACCESS-SIGNATURE", sig)
	req.Header.Set("KALSHI-ACCESS-TIMESTAMP", strconv.FormatInt(ts, 10))

	resp, err := c.http.Do(req)
	if err != nil {
		return typed(ErrUnreachable, "exchange unreachable: "+err.Error())
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return typed(ErrBadPayload, "reading response body failed")
	}
	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return typed(ErrRateLimited, "exchange rate limit reached; agent decides retry timing")
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return typed(ErrUnauthed, "exchange rejected credentials; check KALSHI_API_KEY_ID and key validity")
	case resp.StatusCode >= 400:
		return typed(ErrUpstream, fmt.Sprintf("exchange returned %d: %.512s", resp.StatusCode, string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return typed(ErrBadPayload, "response is not valid expected JSON: "+err.Error())
	}
	return nil
}

// Balance is the live account balance snapshot. Fixed-point strings are
// passed through byte-for-byte; cents ints stay ints.
type Balance struct {
	BalanceDollars   string `json:"balance_dollars"`
	PortfolioDollars string `json:"portfolio_value_dollars"`
	UpdatedTS        int64  `json:"updated_ts"`
}

// Position is one live market position.
type Position struct {
	Ticker             string `json:"ticker"`
	PositionFP         string `json:"position_fp"`
	MarketExposure     string `json:"market_exposure_dollars"`
	RealizedPnlDollars string `json:"realized_pnl_dollars"`
	FeesPaidDollars    string `json:"fees_paid_dollars"`
	LastUpdated        string `json:"last_updated_ts,omitempty"`
}

// RestingOrder is one live order (any status).
type RestingOrder struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id,omitempty"`
	Ticker        string `json:"ticker"`
	Side          string `json:"side"`
	Action        string `json:"action"`
	CountFP       string `json:"count_fp,omitempty"`
	PriceDollars  string `json:"yes_price_dollars,omitempty"`
	Status        string `json:"status"`
	FilledCountFP string `json:"filled_count_fp,omitempty"`
	RemainingFP   string `json:"remaining_count_fp,omitempty"`
	CreatedTime   string `json:"created_time,omitempty"`
}

// Fill is one live execution print.
type Fill struct {
	FillID      string `json:"fill_id"`
	OrderID     string `json:"order_id"`
	Ticker      string `json:"ticker"`
	OutcomeSide string `json:"outcome_side,omitempty"`
	BookSide    string `json:"book_side,omitempty"`
	CountFP     string `json:"count_fp,omitempty"`
	YesPrice    string `json:"yes_price_dollars,omitempty"`
	IsTaker     *bool  `json:"is_taker,omitempty"`
	FeeCost     string `json:"fee_cost_dollars,omitempty"`
	CreatedTime string `json:"created_time,omitempty"`
}

// Portfolio is the aggregated live portfolio read.
type Portfolio struct {
	Balance   Balance        `json:"balance"`
	Positions []Position     `json:"positions"`
	Orders    []RestingOrder `json:"orders"`
	Fills     []Fill         `json:"fills"`
}

// GetPortfolio fetches balance, positions, resting/executed orders, and
// recent fills from the authenticated event-contract account.
func (c *Client) GetPortfolio(ctx context.Context) (*Portfolio, error) {
	p := &Portfolio{}

	var bal struct {
		Balance        int64  `json:"balance"`
		BalanceDollars string `json:"balance_dollars"`
		PortfolioValue int64  `json:"portfolio_value"`
		UpdatedTS      int64  `json:"updated_ts"`
	}
	if err := c.get(ctx, "/trade-api/v2/portfolio/get_balance", nil, &bal); err != nil {
		return nil, err
	}
	p.Balance = Balance{
		BalanceDollars:   firstNonEmpty(bal.BalanceDollars, centsToDollars(bal.Balance)),
		PortfolioDollars: centsToDollars(bal.PortfolioValue),
		UpdatedTS:        bal.UpdatedTS,
	}

	var pos struct {
		MarketPositions []struct {
			Ticker         string `json:"ticker"`
			PositionFP     string `json:"position_fp"`
			MarketExposure string `json:"market_exposure_dollars"`
			RealizedPnL    string `json:"realized_pnl_dollars"`
			FeesPaid       string `json:"fees_paid_dollars"`
			LastUpdated    string `json:"last_updated_ts"`
		} `json:"market_positions"`
	}
	if err := c.get(ctx, "/trade-api/v2/portfolio/get_positions", nil, &pos); err != nil {
		return nil, err
	}
	for _, m := range pos.MarketPositions {
		p.Positions = append(p.Positions, Position{
			Ticker: m.Ticker, PositionFP: m.PositionFP, MarketExposure: m.MarketExposure,
			RealizedPnlDollars: m.RealizedPnL, FeesPaidDollars: m.FeesPaid, LastUpdated: m.LastUpdated,
		})
	}

	var ords struct {
		Orders []struct {
			OrderID       string `json:"order_id"`
			ClientOrderID string `json:"client_order_id"`
			Ticker        string `json:"ticker"`
			Side          string `json:"side"`
			Action        string `json:"action"`
			CountFP       string `json:"count_fp"`
			YesPrice      string `json:"yes_price_dollars"`
			Status        string `json:"status"`
			FilledCountFP string `json:"filled_count_fp"`
			RemainingFP   string `json:"remaining_count_fp"`
			CreatedTime   string `json:"created_time"`
		} `json:"orders"`
	}
	if err := c.get(ctx, "/trade-api/v2/portfolio/get_orders", nil, &ords); err != nil {
		return nil, err
	}
	for _, o := range ords.Orders {
		p.Orders = append(p.Orders, RestingOrder{
			OrderID: o.OrderID, ClientOrderID: o.ClientOrderID, Ticker: o.Ticker,
			Side: o.Side, Action: o.Action, CountFP: o.CountFP, PriceDollars: o.YesPrice,
			Status: o.Status, FilledCountFP: o.FilledCountFP, RemainingFP: o.RemainingFP,
			CreatedTime: o.CreatedTime,
		})
	}

	var fls struct {
		Fills []struct {
			FillID      string `json:"fill_id"`
			OrderID     string `json:"order_id"`
			Ticker      string `json:"ticker"`
			OutcomeSide string `json:"outcome_side"`
			BookSide    string `json:"book_side"`
			CountFP     string `json:"count_fp"`
			YesPrice    string `json:"yes_price_dollars"`
			IsTaker     *bool  `json:"is_taker"`
			FeeCost     string `json:"fee_cost"`
			CreatedTime string `json:"created_time"`
		} `json:"fills"`
	}
	if err := c.get(ctx, "/trade-api/v2/portfolio/get_fills", nil, &fls); err != nil {
		return nil, err
	}
	for _, f := range fls.Fills {
		p.Fills = append(p.Fills, Fill{
			FillID: f.FillID, OrderID: f.OrderID, Ticker: f.Ticker,
			OutcomeSide: f.OutcomeSide, BookSide: f.BookSide, CountFP: f.CountFP,
			YesPrice: f.YesPrice, IsTaker: f.IsTaker, FeeCost: f.FeeCost, CreatedTime: f.CreatedTime,
		})
	}
	return p, nil
}

// LiveOrder is the authoritative exchange-side view of one order.
type LiveOrder struct {
	OrderID        string `json:"order_id"`
	ClientOrderID  string `json:"client_order_id,omitempty"`
	Ticker         string `json:"ticker"`
	OutcomeSide    string `json:"outcome_side,omitempty"`
	BookSide       string `json:"book_side,omitempty"`
	Status         string `json:"status"`
	Type           string `json:"type,omitempty"`
	YesPrice       string `json:"yes_price_dollars,omitempty"`
	FillCountFP    string `json:"fill_count_fp,omitempty"`
	RemainingFP    string `json:"remaining_count_fp,omitempty"`
	InitialCountFP string `json:"initial_count_fp,omitempty"`
}

// GetOrder fetches the authoritative order state from the exchange.
// This is the reconciliation source of truth after any write attempt.
func (c *Client) GetOrder(ctx context.Context, orderID string) (*LiveOrder, error) {
	if orderID == "" {
		return nil, typed(ErrBadInput, "order_id is required")
	}
	var raw struct {
		Order struct {
			OrderID        string `json:"order_id"`
			ClientOrderID  string `json:"client_order_id"`
			Ticker         string `json:"ticker"`
			OutcomeSide    string `json:"outcome_side"`
			BookSide       string `json:"book_side"`
			Status         string `json:"status"`
			Type           string `json:"type"`
			YesPrice       string `json:"yes_price_dollars"`
			FillCountFP    string `json:"fill_count_fp"`
			RemainingFP    string `json:"remaining_count_fp"`
			InitialCountFP string `json:"initial_count_fp"`
		} `json:"order"`
	}
	path := "/trade-api/v2/portfolio/orders/" + url.PathEscape(orderID)
	if err := c.get(ctx, path, nil, &raw); err != nil {
		return nil, err
	}
	o := raw.Order
	return &LiveOrder{
		OrderID: o.OrderID, ClientOrderID: o.ClientOrderID, Ticker: o.Ticker,
		OutcomeSide: o.OutcomeSide, BookSide: o.BookSide, Status: o.Status,
		Type: o.Type, YesPrice: o.YesPrice, FillCountFP: o.FillCountFP,
		RemainingFP: o.RemainingFP, InitialCountFP: o.InitialCountFP,
	}, nil
}

// CancelResult reports what the exchange acknowledged about a cancellation.
type CancelResult struct {
	OrderID       string `json:"order_id"`
	ClientOrderID string `json:"client_order_id,omitempty"`
	ReducedByFP   string `json:"reduced_by_fp"`
	TsMs          int64  `json:"ts_ms"`
}

// CancelOrder cancels the remaining quantity of one resting event-market
// order (DELETE /portfolio/events/orders/{order_id}, V2 response shape).
//
// Indeterminate-outcome contract (docs/THREAT_MODEL.md): a network timeout
// is NEVER reported as a clean failure. On timeout or transport failure we
// immediately re-query the order and report either its true state or an
// explicit outcome_indeterminate error — never a blind retry of the delete.
func (c *Client) CancelOrder(ctx context.Context, orderID, marketTicker string) (*CancelResult, error) {
	if orderID == "" {
		return nil, typed(ErrBadInput, "order_id is required")
	}

	// State gate: only a resting order may be cancelled; this prevents
	// pointless deletes on executed/canceled orders and gives precise
	// typed failures instead of upstream 4xx guesses.
	order, err := c.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}
	switch order.Status {
	case "resting":
		// proceed
	case "canceled":
		return nil, typedf(ErrNotResting, "order %s is already canceled", orderID)
	case "executed":
		return nil, typedf(ErrNotResting, "order %s is fully executed; nothing to cancel", orderID)
	default:
		return nil, typedf(ErrNotResting, "order %s has status %q; only resting orders can be canceled", orderID, order.Status)
	}

	q := url.Values{}
	if marketTicker != "" {
		q.Set("market_ticker", marketTicker)
	}
	path := "/trade-api/v2/portfolio/events/orders/" + url.PathEscape(orderID)

	var raw struct {
		OrderID       string      `json:"order_id"`
		ClientOrderID string      `json:"client_order_id"`
		ReducedBy     json.Number `json:"reduced_by"`
		TsMs          int64       `json:"ts_ms"`
	}
	derr := c.do(ctx, http.MethodDelete, path, q, &raw)
	if derr == nil {
		return &CancelResult{
			OrderID:       raw.OrderID,
			ClientOrderID: raw.ClientOrderID,
			ReducedByFP:   raw.ReducedBy.String(),
			TsMs:          raw.TsMs,
		}, nil
	}

	// Timeout / transport failure: outcome unknown by definition.
	if Code(derr) == ErrUnreachable {
		// The caller's context may be expired; reconciliation needs its
		// own budget. This is a query, not a retry of the delete.
		recheckCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		recheck, qerr := c.GetOrder(recheckCtx, orderID)
		if qerr != nil {
			return nil, typedf(ErrIndeterminate,
				"cancel request for %s timed out and reconciliation query also failed (%s); resolve manually before retrying",
				orderID, Code(qerr))
		}
		switch recheck.Status {
		case "canceled":
			// The cancel actually landed despite the timeout.
			return &CancelResult{OrderID: orderID, ReducedByFP: recheck.RemainingFP}, nil
		case "resting":
			return nil, typedf(ErrIndeterminate,
				"cancel request for %s timed out but order is still resting; safe to retry the cancel explicitly",
				orderID)
		default:
			return nil, typedf(ErrIndeterminate,
				"cancel request for %s timed out and order moved to %q during the attempt",
				orderID, recheck.Status)
		}
	}
	return nil, derr
}

func centsToDollars(cents int64) string {
	if cents == 0 {
		return "0.00"
	}
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return sign + strconv.FormatInt(cents/100, 10) + "." + fmt.Sprintf("%02d", cents%100)
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
