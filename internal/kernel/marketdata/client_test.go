package marketdata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fixtureServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewClientWithBaseURL(server.URL, server.Client())
}

func TestSearchEventMarketsProjectsSummaryFields(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trade-api/v2/markets" {
			t.Errorf("path = %q", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("series_ticker") != "KXHIGHNY" || q.Get("status") != "open" {
			t.Errorf("filters lost: %v", q)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"markets":[{"ticker":"KXHIGHNY-26AUG22-T88",
			"event_ticker":"KXHIGHNY-26AUG22","series_ticker":"KXHIGHNY",
			"title":"High NY","yes_sub_title":"88 or above","status":"open",
			"yes_bid_dollars":"0.5400","yes_ask_dollars":"0.5600",
			"volume_fp":"120.50","open_interest_fp":"3000.00",
			"close_time":"2026-08-22T00:00:00Z"}],"cursor":"next-cursor"}`))
	})
	page, err := client.SearchEventMarkets(context.Background(),
		SearchOptions{SeriesTicker: "KXHIGHNY", Status: "open"})
	if err != nil {
		t.Fatalf("SearchEventMarkets: %v", err)
	}
	if page.Cursor != "next-cursor" || len(page.Markets) != 1 {
		t.Fatalf("page = %+v", page)
	}
	m := page.Markets[0]
	if m.Ticker != "KXHIGHNY-26AUG22-T88" || m.YesBidDollars != "0.5400" || m.VolumeFP != "120.50" {
		t.Fatalf("summary projection wrong: %+v", m)
	}
	// Fixed-point strings must pass through byte-for-byte.
	if m.YesAskDollars != "0.5600" || m.OpenInterestFP != "3000.00" {
		t.Fatalf("fixed-point strings mutated: %+v", m)
	}
}

func TestRateLimitSurfacesTypedError(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	})
	_, err := client.GetEventMarket(context.Background(), "TICKER")
	if Code(err) != errRateLimited {
		t.Fatalf("code = %q, want rate_limited (err=%v)", Code(err), err)
	}
}

func TestUpstreamErrorSurfacesTypedError(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"code":"not_found"}}`))
	})
	_, err := client.GetEventOrderbook(context.Background(), "TICKER", 0)
	if Code(err) != errUpstream {
		t.Fatalf("code = %q, want upstream_error", Code(err))
	}
}

func TestEmptyTickerRejectedBeforeNetwork(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("network call made with empty ticker")
	})
	if _, err := client.GetEventMarket(context.Background(), ""); Code(err) != errBadInput {
		t.Fatalf("code = %q, want invalid_input", Code(err))
	}
}

func TestOrderbookKeepsLevelPairsVerbatim(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("depth") != "3" {
			t.Errorf("depth not passed: %v", r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"orderbook_fp":{"yes_dollars":[["0.5400","10.00"],["0.5300","5.00"]],
			"no_dollars":[["0.4600","8.00"]]}}`))
	})
	ob, err := client.GetEventOrderbook(context.Background(), "TICKER", 3)
	if err != nil {
		t.Fatalf("GetEventOrderbook: %v", err)
	}
	raw, _ := json.Marshal(ob)
	want := `{"yes_dollars":[["0.5400","10.00"],["0.5300","5.00"]],"no_dollars":[["0.4600","8.00"]]}`
	if string(raw) != want {
		t.Fatalf("orderbook mutated:\n got %s\nwant %s", raw, want)
	}
}

func TestSearchMarginMarketsMapsDataField(t *testing.T) {
	client := fixtureServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/trade-api/v2/margin/markets" {
			t.Errorf("path = %q", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"ticker":"M-BTC","title":"BTC perp","status":"open"}],"cursor":""}`))
	})
	page, err := client.SearchMarginMarkets(context.Background(), SearchOptions{})
	if err != nil {
		t.Fatalf("SearchMarginMarkets: %v", err)
	}
	if len(page.Markets) != 1 || page.Markets[0].Ticker != "M-BTC" {
		t.Fatalf("page = %+v", page)
	}
}
