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
	"strings"
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
	errUpstream    = "upstream_error"       // non-200 from exchange
	errRateLimited = "rate_limited"         // 429: caller decides retry
	errBadInput    = "invalid_input"        // request failed validation here
	errUnreachable = "upstream_unreachable" // network/transport failure
	errBadPayload  = "upstream_payload_invalid"
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
	Ticker         string `json:"ticker"`
	EventTicker    string `json:"event_ticker,omitempty"`
	SeriesTicker   string `json:"series_ticker,omitempty"`
	Title          string `json:"title,omitempty"`
	YesSubTitle    string `json:"yes_sub_title,omitempty"`
	Status         string `json:"status,omitempty"`
	YesBidDollars  string `json:"yes_bid_dollars,omitempty"`
	YesAskDollars  string `json:"yes_ask_dollars,omitempty"`
	VolumeFP       string `json:"volume_fp,omitempty"`
	OpenInterestFP string `json:"open_interest_fp,omitempty"`
	CloseTime      string `json:"close_time,omitempty"`
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

// OHLC is an open/high/low/close distribution of fixed-point dollar
// strings. Traded-price distributions may be null when no trade occurred
// in the bucket; quote (bid/ask) distributions are required by upstream.
type OHLC struct {
	OpenDollars  *string `json:"open_dollars,omitempty"`
	HighDollars  *string `json:"high_dollars,omitempty"`
	LowDollars   *string `json:"low_dollars,omitempty"`
	CloseDollars *string `json:"close_dollars,omitempty"`
}

// Candlestick is one OHLC bucket exactly as upstream emits it: prices are
// fixed-point dollar strings, volume/open interest are fixed-point strings.
// price may be nil when no trade printed during the period.
type Candlestick struct {
	EndPeriodTS int64  `json:"end_period_ts"`
	Price       *OHLC  `json:"price,omitempty"`
	YesBid      *OHLC  `json:"yes_bid,omitempty"`
	YesAsk      *OHLC  `json:"yes_ask,omitempty"`
	VolumeFP    string `json:"volume_fp,omitempty"`
	OpenIntFP   string `json:"open_interest_fp,omitempty"`
}

// CandlesPage is the candlestick response with echo metadata.
type CandlesPage struct {
	Ticker        string        `json:"ticker"`
	PeriodMinutes int           `json:"period_interval_minutes"`
	Candlesticks  []Candlestick `json:"candlesticks"`
}

// CandleOptions carries required candlestick window parameters.
type CandleOptions struct {
	StartTS                  int64
	EndTS                    int64
	PeriodIntervalMinutes    int
	IncludeLatestBeforeStart bool
}

func validateCandleOptions(opt CandleOptions) error {
	if opt.StartTS <= 0 || opt.EndTS <= 0 {
		return typed(errBadInput, "start_ts and end_ts are required unix timestamps")
	}
	if opt.EndTS < opt.StartTS {
		return typed(errBadInput, "end_ts must be >= start_ts")
	}
	switch opt.PeriodIntervalMinutes {
	case 1, 60, 1440:
	default:
		return typed(errBadInput, "period_interval must be 1, 60, or 1440 minutes")
	}
	return nil
}

func candleQuery(opt CandleOptions) url.Values {
	q := url.Values{}
	q.Set("start_ts", strconv.FormatInt(opt.StartTS, 10))
	q.Set("end_ts", strconv.FormatInt(opt.EndTS, 10))
	q.Set("period_interval", strconv.Itoa(opt.PeriodIntervalMinutes))
	if opt.IncludeLatestBeforeStart {
		q.Set("include_latest_before_start", "true")
	}
	return q
}

func decodeCandles(raw json.RawMessage) ([]Candlestick, error) {
	var parsed struct {
		Candlesticks []struct {
			EndPeriodTS int64       `json:"end_period_ts"`
			Price       *OHLC       `json:"price"`
			YesBid      *OHLC       `json:"yes_bid"`
			YesAsk      *OHLC       `json:"yes_ask"`
			VolumeFP    json.Number `json:"volume_fp"`
			OpenIntFP   json.Number `json:"open_interest_fp"`
		} `json:"candlesticks"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, typed(errBadPayload, "unexpected candlestick payload: "+err.Error())
	}
	out := make([]Candlestick, 0, len(parsed.Candlesticks))
	for _, c := range parsed.Candlesticks {
		out = append(out, Candlestick{
			EndPeriodTS: c.EndPeriodTS,
			Price:       c.Price,
			YesBid:      c.YesBid,
			YesAsk:      c.YesAsk,
			VolumeFP:    c.VolumeFP.String(),
			OpenIntFP:   c.OpenIntFP.String(),
		})
	}
	return out, nil
}

// GetEventCandles fetches candlesticks for an event-contract market.
// seriesTicker is the market's parent series (upstream path component).
func (c *Client) GetEventCandles(ctx context.Context, seriesTicker, ticker string, opt CandleOptions) (*CandlesPage, error) {
	if seriesTicker == "" || ticker == "" {
		return nil, typed(errBadInput, "series_ticker and ticker are required")
	}
	if err := validateCandleOptions(opt); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	path := "/trade-api/v2/series/" + url.PathEscape(seriesTicker) +
		"/markets/" + url.PathEscape(ticker) + "/candlesticks"
	if err := get(ctx, c, path, candleQuery(opt), &raw); err != nil {
		return nil, err
	}
	candles, err := decodeCandles(raw)
	if err != nil {
		return nil, err
	}
	return &CandlesPage{
		Ticker:        ticker,
		PeriodMinutes: opt.PeriodIntervalMinutes,
		Candlesticks:  candles,
	}, nil
}

// GetMarginCandles fetches candlesticks for a perpetuals market.
func (c *Client) GetMarginCandles(ctx context.Context, ticker string, opt CandleOptions) (*CandlesPage, error) {
	if ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	if err := validateCandleOptions(opt); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	path := "/trade-api/v2/margin/markets/" + url.PathEscape(ticker) + "/candlesticks"
	if err := get(ctx, c, path, candleQuery(opt), &raw); err != nil {
		return nil, err
	}
	candles, err := decodeCandles(raw)
	if err != nil {
		return nil, err
	}
	return &CandlesPage{
		Ticker:        ticker,
		PeriodMinutes: opt.PeriodIntervalMinutes,
		Candlesticks:  candles,
	}, nil
}

// Trade is one public print on the tape, verbatim fixed-point strings.
type Trade struct {
	Ticker       string `json:"ticker"`
	PriceDollars string `json:"yes_price_dollars,omitempty"`
	CountFP      string `json:"count_fp,omitempty"`
	TradedAt     string `json:"created_time,omitempty"`
	IsBlockTrade *bool  `json:"is_block_trade,omitempty"`
	TradeID      string `json:"trade_id,omitempty"`
}

// TradesPage is one page of the trade tape with passthrough cursor.
type TradesPage struct {
	Trades []Trade `json:"trades"`
	Cursor string  `json:"cursor,omitempty"`
}

// TradesOptions maps curated get_trades filters onto upstream parameters.
type TradesOptions struct {
	Ticker       string
	MinTS        int64
	MaxTS        int64
	Limit        int
	Cursor       string
	IsBlockTrade *bool
}

// GetEventTrades fetches one page of the public event-contract trade tape.
func (c *Client) GetEventTrades(ctx context.Context, opt TradesOptions) (*TradesPage, error) {
	if opt.Ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	q := url.Values{}
	q.Set("ticker", opt.Ticker)
	if opt.MinTS > 0 {
		q.Set("min_ts", strconv.FormatInt(opt.MinTS, 10))
	}
	if opt.MaxTS > 0 {
		q.Set("max_ts", strconv.FormatInt(opt.MaxTS, 10))
	}
	if opt.Limit > 0 {
		q.Set("limit", strconv.Itoa(opt.Limit))
	}
	setIfNotEmpty(q, "cursor", opt.Cursor)
	if opt.IsBlockTrade != nil {
		q.Set("is_block_trade", strconv.FormatBool(*opt.IsBlockTrade))
	}
	var raw struct {
		Trades []struct {
			Ticker       string `json:"ticker"`
			YesPrice     string `json:"yes_price_dollars"`
			CountFP      string `json:"count_fp"`
			CreatedTime  string `json:"created_time"`
			IsBlockTrade *bool  `json:"is_block_trade"`
			TradeID      string `json:"trade_id"`
		} `json:"trades"`
		Cursor string `json:"cursor"`
	}
	if err := get(ctx, c, "/trade-api/v2/markets/trades", q, &raw); err != nil {
		return nil, err
	}
	page := &TradesPage{Cursor: raw.Cursor}
	for _, t := range raw.Trades {
		page.Trades = append(page.Trades, Trade{
			Ticker: t.Ticker, PriceDollars: t.YesPrice, CountFP: t.CountFP,
			TradedAt: t.CreatedTime, IsBlockTrade: t.IsBlockTrade, TradeID: t.TradeID,
		})
	}
	return page, nil
}

// fixedPoint is a JSON number decoded and re-encoded as its exact source
// token (e.g. 91.40 stays "91.40"), preserving upstream fixed-point fidelity.
type fixedPoint string

func (f *fixedPoint) UnmarshalJSON(data []byte) error {
	*f = fixedPoint(strings.TrimSpace(string(data)))
	return nil
}

func (f fixedPoint) MarshalJSON() ([]byte, error) {
	return []byte(f), nil
}

func (f fixedPoint) String() string { return string(f) }

// WeatherIndexStationReading is one member station's reported reading and QC
// disposition, exactly as upstream emits it (only with detailed=true).
// temp_f/source are nil: absent members carry no reading.
type WeatherIndexStationReading struct {
	StationID string      `json:"station_id"`
	Code      string      `json:"code"`
	Source    *string     `json:"source,omitempty"`
	TempF     *fixedPoint `json:"temp_f,omitempty"`
}

// WeatherIndexPoint is one minute of the city temperature index. v and
// contributors are nil on `incomplete` points, which have no canonical value
// and no contributor count — they are returned as points but never
// zero-filled. Minutes where quorum failed carry no point at all.
type WeatherIndexPoint struct {
	T            int64                        `json:"t"`
	V            *fixedPoint                  `json:"v,omitempty"`
	Status       string                       `json:"status"`
	Contributors *int                         `json:"contributors,omitempty"`
	Stations     []WeatherIndexStationReading `json:"stations,omitempty"`
}

// WeatherIndex is the Kalshi-computed city temperature index response.
type WeatherIndex struct {
	City          string              `json:"city"`
	ConfigVersion string              `json:"config_version,omitempty"`
	Units         string              `json:"units"`
	Timeseries    []WeatherIndexPoint `json:"timeseries"`
}

// GetWeatherIndex fetches the Kalshi-computed city temperature index via
// GET /live_data/weather/{city}. Exactly one of lastSec or the
// from/to pair may be supplied; from without to (or vice versa) is invalid.
func (c *Client) GetWeatherIndex(ctx context.Context, city string, from, to, lastSec *int64, detailed bool) (*WeatherIndex, error) {
	if city == "" {
		return nil, typed(errBadInput, "city is required")
	}
	if lastSec != nil {
		if from != nil || to != nil {
			return nil, typed(errBadInput, "last_sec is mutually exclusive with from/to")
		}
	} else if (from == nil) != (to == nil) {
		return nil, typed(errBadInput, "from and to must be supplied together")
	}
	query := url.Values{}
	if lastSec != nil {
		query.Set("last_sec", strconv.FormatInt(*lastSec, 10))
	}
	if from != nil && to != nil {
		query.Set("from", strconv.FormatInt(*from, 10))
		query.Set("to", strconv.FormatInt(*to, 10))
	}
	if detailed {
		query.Set("detailed", "true")
	}
	var index WeatherIndex
	path := "/trade-api/v2/live_data/weather/" + url.PathEscape(city)
	if err := get(ctx, c, path, query, &index); err != nil {
		return nil, err
	}
	return &index, nil
}

// LastQuote is a compact live pricing snapshot for one market. Every
// price/size field is the fixed-point string exactly as upstream emits it.
type LastQuote struct {
	Ticker string `json:"ticker"`
	Status string `json:"status,omitempty"`

	// Event-contract fields (empty for perps).
	LastPriceDollars string `json:"last_price_dollars,omitempty"`
	YesBidDollars    string `json:"yes_bid_dollars,omitempty"`
	YesAskDollars    string `json:"yes_ask_dollars,omitempty"`
	YesBidSizeFP     string `json:"yes_bid_size_fp,omitempty"`
	YesAskSizeFP     string `json:"yes_ask_size_fp,omitempty"`
	NoBidDollars     string `json:"no_bid_dollars,omitempty"`
	NoAskDollars     string `json:"no_ask_dollars,omitempty"`

	// Perpetuals fields (empty for event contracts).
	MarkPriceDollars       string `json:"mark_price_dollars,omitempty"`
	BidDollars             string `json:"bid_dollars,omitempty"`
	AskDollars             string `json:"ask_dollars,omitempty"`
	SettlementMarkDollars  string `json:"settlement_mark_price_dollars,omitempty"`
	LiquidationMarkDollars string `json:"liquidation_mark_price_dollars,omitempty"`

	Volume24hFP string `json:"volume_24h_fp,omitempty"`
}

// GetEventLastQuote fetches the compact live pricing snapshot for one
// event-contract market via GET /markets/{ticker}.
func (c *Client) GetEventLastQuote(ctx context.Context, ticker string) (*LastQuote, error) {
	if ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	var raw struct {
		Market struct {
			Ticker       string `json:"ticker"`
			Status       string `json:"status"`
			LastPrice    string `json:"last_price_dollars"`
			YesBid       string `json:"yes_bid_dollars"`
			YesAsk       string `json:"yes_ask_dollars"`
			YesBidSizeFP string `json:"yes_bid_size_fp"`
			YesAskSizeFP string `json:"yes_ask_size_fp"`
			NoBid        string `json:"no_bid_dollars"`
			NoAsk        string `json:"no_ask_dollars"`
			Volume24hFP  string `json:"volume_24h_fp"`
		} `json:"market"`
	}
	path := "/trade-api/v2/markets/" + url.PathEscape(ticker)
	if err := get(ctx, c, path, nil, &raw); err != nil {
		return nil, err
	}
	m := raw.Market
	return &LastQuote{
		Ticker:           m.Ticker,
		Status:           m.Status,
		LastPriceDollars: m.LastPrice,
		YesBidDollars:    m.YesBid,
		YesAskDollars:    m.YesAsk,
		YesBidSizeFP:     m.YesBidSizeFP,
		YesAskSizeFP:     m.YesAskSizeFP,
		NoBidDollars:     m.NoBid,
		NoAskDollars:     m.NoAsk,
		Volume24hFP:      m.Volume24hFP,
	}, nil
}

// GetMarginLastQuote fetches the compact live pricing snapshot for one
// perpetuals market via GET /margin/markets.
func (c *Client) GetMarginLastQuote(ctx context.Context, ticker string) (*LastQuote, error) {
	if ticker == "" {
		return nil, typed(errBadInput, "ticker is required")
	}
	q := url.Values{}
	q.Set("tickers", ticker)
	var raw struct {
		Markets []struct {
			Ticker         string      `json:"ticker"`
			Status         string      `json:"status"`
			Price          json.Number `json:"price"`
			Bid            json.Number `json:"bid"`
			Ask            json.Number `json:"ask"`
			SettlementMark struct {
				Price json.Number `json:"price"`
			} `json:"settlement_mark_price"`
			LiquidationMark struct {
				Price json.Number `json:"price"`
			} `json:"liquidation_mark_price"`
			Volume24h json.Number `json:"volume_24h"`
		} `json:"markets"`
	}
	if err := get(ctx, c, "/trade-api/v2/margin/markets", q, &raw); err != nil {
		return nil, err
	}
	if len(raw.Markets) == 0 {
		return nil, typed(errUpstream, "perp market not found: "+ticker)
	}
	m := raw.Markets[0]
	return &LastQuote{
		Ticker:                 m.Ticker,
		Status:                 m.Status,
		MarkPriceDollars:       m.Price.String(),
		BidDollars:             m.Bid.String(),
		AskDollars:             m.Ask.String(),
		SettlementMarkDollars:  m.SettlementMark.Price.String(),
		LiquidationMarkDollars: m.LiquidationMark.Price.String(),
		Volume24hFP:            m.Volume24h.String(),
	}, nil
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
