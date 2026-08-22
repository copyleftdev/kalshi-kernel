// Package marketdata provides a read-only HTTP adapter for Kalshi public
// market-data endpoints. It is transport-only: no credentials are used or
// stored, all prices and quantities remain fixed-point strings, and every
// failure is surfaced as a typed error for the caller to decide on retries.
package marketdata

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	defaultBaseURL  = "https://api.elections.kalshi.com"
	defaultTimeout  = 10 * time.Second
	userAgent       = "kalshi-kernel/0.1.1 (read-only market data)"
	maxResponseSize = 8 << 20 // 8 MiB cap per response
)

// Client performs unauthenticated GET requests against Kalshi public
// market-data endpoints.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient returns a Client bound to the production Kalshi API host.
func NewClient() *Client {
	return &Client{
		baseURL: defaultBaseURL,
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

// NewClientWithBaseURL is used by tests to point the client at a fixture
// server.
func NewClientWithBaseURL(baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultTimeout}
	}
	return &Client{baseURL: baseURL, http: httpClient}
}

// Typed error codes surfaced through mcptools.Response.Error.Code.
const (
	errUpstream      = "upstream_error"      // non-200 from exchange
	errRateLimited   = "rate_limited"        // 429: caller decides retry
	errBadInput      = "invalid_input"       // request failed validation here
	errUnreachable   = "upstream_unreachable" // network/transport failure
	errBadPayload    = "upstream_payload_invalid"
)

func get(ctx context.Context, c *Client, path string, query url.Values, out any) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return typed(errBadInput, "request construction failed: "+err.Error())
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return typed(errUnreachable, "exchange unreachable: "+err.Error())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize))
	if err != nil {
		return typed(errBadPayload, "reading response body failed: "+err.Error())
	}

	switch {
	case resp.StatusCode == http.StatusTooManyRequests:
		return typed(errRateLimited, "exchange rate limit reached; agent decides retry timing")
	case resp.StatusCode >= 400:
		return typed(errUpstream, fmt.Sprintf("exchange returned %d: %.512s", resp.StatusCode, string(body)))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return typed(errBadPayload, "response is not valid expected JSON: "+err.Error())
	}
	return nil
}

type typedError struct {
	code    string
	message string
}

func (e *typedError) Error() string { return e.code + ": " + e.message }

func Code(err error) string {
	if te, ok := err.(*typedError); ok {
		return te.code
	}
	return "internal_error"
}

func typed(code, message string) error { return &typedError{code: code, message: message} }

// MarketSummary is a compact projection of an event-contract market,
// sized for agent context budgets. Prices and sizes stay fixed-point
// strings exactly as upstream emits them.
type MarketSummary struct {
	Ticker          string `json:"ticker"`
	EventTicker     string `json:"event_ticker,omitempty"`
	SeriesTicker    string `json:"series_ticker,omitempty"`
	Title           string `json:"title,omitempty"`
	YesSubTitle     string `json:"yes_sub_title,omitempty"`
	Status          string `json:"status,omitempty"`
	YesBidDollars   string `json:"yes_bid_dollars,omitempty"`
	YesAskDollars   string `json:"yes_ask_dollars,omitempty"`
	VolumeFP        string `json:"volume_fp,omitempty"`
	OpenInterestFP  string `json:"open_interest_fp,omitempty"`
	CloseTime       string `json:"close_time,omitempty"`
}

// MarketsPage is one page of search results with the passthrough cursor.
type MarketsPage struct {
	Markets []MarketSummary `json:"markets"`
	Cursor  string          `json:"cursor,omitempty"`
}

// SearchOptions maps curated search_markets filters onto upstream query
// parameters. Tickers joins into the upstream comma-separated form.
type SearchOptions struct {
	Tickers      []string
	EventTicker  string
	SeriesTicker string
	Status       string
	Limit        int
	Cursor       string
}

// SearchEventMarkets fetches one page of event-contract markets.
func (c *Client) SearchEventMarkets(ctx context.Context, opt SearchOptions) (*MarketsPage, error) {
	q := url.Values{}
	if len(opt.Tickers) > 0 {
		q.Set("tickers", joinComma(opt.Tickers))
	}
	setIfNotEmpty(q, "event_ticker", opt.EventTicker)
	setIfNotEmpty(q, "series_ticker", opt.SeriesTicker)
	setIfNotEmpty(q, "status", opt.Status)
	if opt.Limit > 0 {
		q.Set("limit", strconv.Itoa(opt.Limit))
	}
	setIfNotEmpty(q, "cursor", opt.Cursor)

	var raw struct {
		Markets []struct {
			Ticker         string `json:"ticker"`
			EventTicker    string `json:"event_ticker"`
			SeriesTicker   string `json:"series_ticker"`
			Title          string `json:"title"`
			YesSubTitle    string `json:"yes_sub_title"`
			Status         string `json:"status"`
			YesBidDollars  string `json:"yes_bid_dollars"`
			YesAskDollars  string `json:"yes_ask_dollars"`
			VolumeFP       string `json:"volume_fp"`
			OpenInterestFP string `json:"open_interest_fp"`
			CloseTime      string `json:"close_time"`
		} `json:"markets"`
		Cursor string `json:"cursor"`
	}
	if err := get(ctx, c, "/trade-api/v2/markets", q, &raw); err != nil {
		return nil, err
	}
	page := &MarketsPage{Cursor: raw.Cursor}
	for _, m := range raw.Markets {
		page.Markets = append(page.Markets, MarketSummary{
			Ticker: m.Ticker, EventTicker: m.EventTicker, SeriesTicker: m.SeriesTicker,
			Title: m.Title, YesSubTitle: m.YesSubTitle, Status: m.Status,
			YesBidDollars: m.YesBidDollars, YesAskDollars: m.YesAskDollars,
			VolumeFP: m.VolumeFP, OpenInterestFP: m.OpenInterestFP, CloseTime: m.CloseTime,
		})
	}
	return page, nil
}

// SearchMarginMarkets fetches one page of perpetuals markets.
func (c *Client) SearchMarginMarkets(ctx context.Context, opt SearchOptions) (*MarketsPage, error) {
	q := url.Values{}
	if len(opt.Tickers) > 0 {
		q.Set("tickers", joinComma(opt.Tickers))
	}
	setIfNotEmpty(q, "status", opt.Status)
	var raw struct {
		Markets []map[string]any `json:"data"`
		Cursor  string           `json:"cursor"`
	}
	if err := get(ctx, c, "/trade-api/v2/margin/markets", q, &raw); err != nil {
		return nil, err
	}
	page := &MarketsPage{Cursor: raw.Cursor}
	for _, m := range raw.Markets {
		page.Markets = append(page.Markets, MarketSummary{
			Ticker: str(m["ticker"]), Title: str(m["title"]), Status: str(m["status"]),
		})
	}
	return page, nil
}

// GetEventMarket returns authoritative metadata for one event-contract
// market as raw JSON (schema-faithful; no lossy re-typing).
func (c *Client) GetEventMarket(ctx context.Context, ticker string) (json.RawMessage, error) {
	if ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	var raw json.RawMessage
	path := "/trade-api/v2/markets/" + url.PathEscape(ticker)
	if err := get(ctx, c, path, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// GetMarginMarket returns authoritative metadata for one perpetuals market.
func (c *Client) GetMarginMarket(ctx context.Context, ticker string) (json.RawMessage, error) {
	if ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	var raw json.RawMessage
	path := "/trade-api/v2/margin/markets/" + url.PathEscape(ticker)
	if err := get(ctx, c, path, nil, &raw); err != nil {
		return nil, err
	}
	return raw, nil
}

// PriceLevel is [dollars_string, fp_count] verbatim from upstream.
type PriceLevel []string

// Orderbook keeps both sides exactly as upstream emits them:
// yes bids and no bids only (a yes ask is a bid on the no side).
type Orderbook struct {
	Yes []PriceLevel `json:"yes_dollars"`
	No  []PriceLevel `json:"no_dollars"`
}

// GetEventOrderbook fetches the order book for an event-contract market.
// Depth 0 means upstream default.
func (c *Client) GetEventOrderbook(ctx context.Context, ticker string, depth int) (*Orderbook, error) {
	if ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	q := url.Values{}
	if depth > 0 {
		q.Set("depth", strconv.Itoa(depth))
	}
	var raw struct {
		Orderbook Orderbook `json:"orderbook_fp"`
	}
	path := "/trade-api/v2/markets/" + url.PathEscape(ticker) + "/orderbook"
	if err := get(ctx, c, path, q, &raw); err != nil {
		return nil, err
	}
	ob := raw.Orderbook
	return &ob, nil
}

// GetMarginOrderbook fetches the order book for a perpetuals market.
func (c *Client) GetMarginOrderbook(ctx context.Context, ticker string, depth int) (*Orderbook, error) {
	if ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	q := url.Values{}
	if depth > 0 {
		q.Set("depth", strconv.Itoa(depth))
	}
	var raw struct {
		Orderbook Orderbook `json:"orderbook_fp"`
	}
	path := "/trade-api/v2/margin/markets/" + url.PathEscape(ticker) + "/orderbook"
	if err := get(ctx, c, path, q, &raw); err != nil {
		return nil, err
	}
	ob := raw.Orderbook
	return &ob, nil
}

func setIfNotEmpty(q url.Values, key, val string) {
	if val != "" {
		q.Set(key, val)
	}
}

func joinComma(items []string) string {
	out := ""
	for i, s := range items {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

func str(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
