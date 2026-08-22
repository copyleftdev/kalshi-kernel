package marketdata

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestGetEventCandlesHappyPath(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trade-api/v2/series/KXHIGHNY/markets/KXHIGHNY-26AUG22-T88/candlesticks" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("start_ts") != "1000" || q.Get("end_ts") != "2000" || q.Get("period_interval") != "60" {
			t.Errorf("query lost: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"candlesticks":[
			{"end_period_ts":3600,"price":{"open_dollars":"0.5400","high_dollars":"0.5800","low_dollars":"0.5200","close_dollars":"0.5600"},"yes_bid":{"open_dollars":"0.5200","low_dollars":"0.5200","high_dollars":"0.5500","close_dollars":"0.5500"},"yes_ask":{"open_dollars":"0.5700","low_dollars":"0.5500","high_dollars":"0.5800","close_dollars":"0.5800"},"volume_fp":120.50,"open_interest_fp":3000.00},
			{"end_period_ts":7200,"price":{"open_dollars":null,"high_dollars":null,"low_dollars":null,"close_dollars":null,"previous_dollars":"0.5600"},"yes_bid":{"open_dollars":"0.5000","low_dollars":"0.5000","high_dollars":"0.5000","close_dollars":"0.5000"},"yes_ask":{"open_dollars":"0.6000","low_dollars":"0.6000","high_dollars":"0.6000","close_dollars":"0.6000"},"volume_fp":0}
		]}`))
	})
	page, err := client.GetEventCandles(context.Background(), "KXHIGHNY", "KXHIGHNY-26AUG22-T88",
		CandleOptions{StartTS: 1000, EndTS: 2000, PeriodIntervalMinutes: 60})
	if err != nil {
		t.Fatalf("GetEventCandles: %v", err)
	}
	if len(page.Candlesticks) != 2 || page.PeriodMinutes != 60 || page.Ticker != "KXHIGHNY-26AUG22-T88" {
		t.Fatalf("page = %+v", page)
	}
	c := page.Candlesticks[0]
	// Fixed-point strings must pass through byte-for-byte.
	if c.Price == nil || c.Price.OpenDollars == nil || *c.Price.OpenDollars != "0.5400" || c.VolumeFP != "120.50" {
		t.Fatalf("candle projection wrong: %+v", c)
	}
	if c.YesBid == nil || *c.YesBid.CloseDollars != "0.5500" {
		t.Fatalf("yes_bid projection wrong: %+v", c.YesBid)
	}
	// No-trade bucket: traded price OHLC stays null (pointer), no invented values.
	syn := page.Candlesticks[1]
	if syn.Price == nil || syn.Price.OpenDollars != nil || syn.Price.CloseDollars != nil {
		t.Fatalf("no-trade bucket price OHLC must remain null: %+v", syn)
	}
}

func TestGetMarginCandlesPathAndValidation(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/trade-api/v2/margin/markets/BTC-27AUG/candlesticks") {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"candlesticks":[{"end_period_ts":86400,"price":{"open_dollars":"60000.00","high_dollars":"60100.00","low_dollars":"59900.00","close_dollars":"60050.00"},"volume_fp":1}]}`))
	})
	page, err := client.GetMarginCandles(context.Background(), "BTC-27AUG",
		CandleOptions{StartTS: 10, EndTS: 20, PeriodIntervalMinutes: 1440})
	if err != nil {
		t.Fatalf("GetMarginCandles: %v", err)
	}
	if len(page.Candlesticks) != 1 || *page.Candlesticks[0].Price.CloseDollars != "60050.00" {
		t.Fatalf("page = %+v", page)
	}

	// Input validation is local and typed — never an upstream round-trip.
	bad := CandleOptions{StartTS: 10, EndTS: 5, PeriodIntervalMinutes: 60}
	if _, err = client.GetEventCandles(context.Background(), "S", "M", bad); Code(err) != errBadInput {
		t.Fatalf("end<start: got %v", err)
	}
	bad.PeriodIntervalMinutes = 15
	if _, err = client.GetMarginCandles(context.Background(), "BTC-27AUG", bad); Code(err) != errBadInput {
		t.Fatalf("period 15: got %v", err)
	}
}

func TestGetTradesProjectsFixedPoint(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trade-api/v2/markets/trades" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("ticker") != "KXHIGHNY-26AUG22-T88" || q.Get("min_ts") != "100" ||
			q.Get("limit") != "50" || q.Get("is_block_trade") != "false" {
			t.Errorf("filters lost: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"trades":[
			{"ticker":"KXHIGHNY-26AUG22-T88","yes_price_dollars":"0.5500","count_fp":"10.00","created_time":"2026-08-22T03:00:00Z","is_block_trade":false,"trade_id":"abc123"}
		],"cursor":"c-2"}`))
	})
	blockFalse := false
	page, err := client.GetEventTrades(context.Background(), TradesOptions{
		Ticker: "KXHIGHNY-26AUG22-T88", MinTS: 100, Limit: 50, IsBlockTrade: &blockFalse,
	})
	if err != nil {
		t.Fatalf("GetEventTrades: %v", err)
	}
	if page.Cursor != "c-2" || len(page.Trades) != 1 {
		t.Fatalf("page = %+v", page)
	}
	tr := page.Trades[0]
	if tr.PriceDollars != "0.5500" || tr.CountFP != "10.00" || tr.TradeID != "abc123" {
		t.Fatalf("trade projection wrong: %+v", tr)
	}
	if tr.IsBlockTrade == nil || *tr.IsBlockTrade {
		t.Fatalf("block flag wrong: %+v", tr.IsBlockTrade)
	}
}

func TestGetTradesRequiresTicker(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called without a ticker")
	})
	if _, err := client.GetEventTrades(context.Background(), TradesOptions{}); Code(err) != errBadInput {
		t.Fatalf("got %v", err)
	}
}

func TestCandlesRateLimitedIsTyped(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.GetEventCandles(context.Background(), "S", "M",
		CandleOptions{StartTS: 1, EndTS: 2, PeriodIntervalMinutes: 1})
	if Code(err) != errRateLimited {
		t.Fatalf("got %v", err)
	}
}
