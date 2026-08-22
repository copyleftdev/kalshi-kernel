package marketdata

import (
	"context"
	"net/http"
	"testing"
)

func TestGetEventLastQuoteProjectsSnapshot(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trade-api/v2/markets/KXHIGHNY-26AUG23-T87" {
			t.Errorf("path = %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"market":{
			"ticker":"KXHIGHNY-26AUG23-T87","status":"active",
			"last_price_dollars":"0.0100",
			"yes_bid_dollars":"0.0100","yes_ask_dollars":"0.0200",
			"yes_bid_size_fp":"310.00","yes_ask_size_fp":"120.50",
			"no_bid_dollars":"0.9800","no_ask_dollars":"0.9900",
			"volume_24h_fp":"1313.90"
		}}`))
	})
	q, err := client.GetEventLastQuote(context.Background(), "KXHIGHNY-26AUG23-T87")
	if err != nil {
		t.Fatalf("GetEventLastQuote: %v", err)
	}
	if q.LastPriceDollars != "0.0100" || q.YesBidDollars != "0.0100" || q.YesAskDollars != "0.0200" {
		t.Fatalf("quote wrong: %+v", q)
	}
	if q.YesBidSizeFP != "310.00" || q.Volume24hFP != "1313.90" || q.NoBidDollars != "0.9800" {
		t.Fatalf("fixed-point passthrough broken: %+v", q)
	}
	if q.BidDollars != "" || q.MarkPriceDollars != "" {
		t.Fatalf("perp fields must stay empty for event product: %+v", q)
	}
}

func TestGetMarginLastQuoteProjectsSnapshot(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trade-api/v2/margin/markets" || r.URL.Query().Get("tickers") != "KXBCHPERP" {
			t.Errorf("request = %s?%s", r.URL.Path, r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markets":[{
			"ticker":"KXBCHPERP","status":"active",
			"price":"2.7947","bid":"2.7935","ask":"2.7956",
			"settlement_mark_price":{"price":"2.7948","ts_ms":1787431516649},
			"liquidation_mark_price":{"price":"2.7938","ts_ms":1787431519000},
			"volume_24h":"6217904.00"
		}]}`))
	})
	q, err := client.GetMarginLastQuote(context.Background(), "KXBCHPERP")
	if err != nil {
		t.Fatalf("GetMarginLastQuote: %v", err)
	}
	// json.Number passthrough keeps upstream digits byte-for-byte.
	if q.MarkPriceDollars != "2.7947" || q.BidDollars != "2.7935" || q.AskDollars != "2.7956" {
		t.Fatalf("quote wrong: %+v", q)
	}
	if q.SettlementMarkDollars != "2.7948" || q.Volume24hFP != "6217904.00" {
		t.Fatalf("snapshot fields wrong: %+v", q)
	}
	if q.LastPriceDollars != "" {
		t.Fatalf("event fields must stay empty for perp product: %+v", q)
	}
}

func TestGetMarginLastQuoteMissingMarketIsTyped(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markets":[]}`))
	})
	_, err := client.GetMarginLastQuote(context.Background(), "NOPE")
	if Code(err) != errUpstream {
		t.Fatalf("got %v", err)
	}
}

func TestGetLastQuoteRequiresTicker(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Error("upstream must not be called without a ticker")
	})
	if _, err := client.GetEventLastQuote(context.Background(), ""); Code(err) != errBadInput {
		t.Fatalf("got %v", err)
	}
}
