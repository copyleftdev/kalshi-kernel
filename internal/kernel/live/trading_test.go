package live

import (
	"context"
	"encoding/json"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func armedSetup(t *testing.T) (*Client, *ArmState, *RiskTracker) {
	t.Helper()
	key := testKey(t)
	client := NewWithBaseURL("test-key", key, "", nil)
	arm := &ArmState{}
	tracker, err := NewRiskTracker(DefaultRiskLimits)
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	return client, arm, tracker
}

func TestPlaceRequiresArm(t *testing.T) {
	client, arm, tracker := armedSetup(t)
	_ = client
	req := PlaceRequest{
		Ticker: "T", ClientOrderID: "c1", Side: "bid", CountFP: "1.00",
		PriceDollars: "0.50", TimeInForce: "good_till_canceled", SelfTradePreventionType: "maker",
	}
	if _, err := client.PlaceOrder(context.Background(), req, arm, nil, tracker); Code(err) != ErrDisarmed {
		t.Fatalf("unarmed place: got %q (%v)", Code(err), err)
	}
	if err := arm.Arm("wrong phrase"); Code(err) != ErrDisarmed {
		t.Fatalf("bad ack accepted: %v", err)
	}
	if arm.Armed() {
		t.Fatal("arm state changed on bad ack")
	}
}

func TestPlaceHappyPathAndBodyShape(t *testing.T) {
	key := testKey(t)
	var gotBody map[string]any
	var sawPost atomic.Bool
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		verifySignature(t, key, r)
		if r.Method == http.MethodPost && r.URL.Path == "/trade-api/v2/portfolio/events/orders" {
			sawPost.Store(true)
			if err := jsonDecode(r, &gotBody); err != nil {
				t.Errorf("body: %v", err)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"order_id":"o9","client_order_id":"c1","fill_count":"0.00","remaining_count":"2.00","ts_ms":1715793600123}`))
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	})
	_, arm, tracker := armedSetup(t)
	if err := arm.Arm("I_UNDERSTAND_THIS_TRADES_REAL_MONEY"); err != nil {
		t.Fatalf("arm: %v", err)
	}
	res, err := client.PlaceOrder(context.Background(), PlaceRequest{
		Ticker: "TICK", ClientOrderID: "c1", Side: "bid", CountFP: "2.00",
		PriceDollars: "0.50", TimeInForce: "good_till_canceled", SelfTradePreventionType: "maker",
	}, arm, nil, tracker)
	if err != nil {
		t.Fatalf("PlaceOrder: %v", err)
	}
	if !sawPost.Load() || res.OrderID != "o9" || res.RemainingFP != "2.00" {
		t.Fatalf("res = %+v", res)
	}
	if gotBody["ticker"] != "TICK" || gotBody["side"] != "bid" || gotBody["count"] != "2.00" || gotBody["price"] != "0.50" {
		t.Fatalf("body wrong: %#v", gotBody)
	}
	// Risk tracker recorded the fill: 2 contracts * $0.50 = $1.00 notional.
	orders, notional := tracker.Snapshot()
	if orders != 1 || notional != "1.00" {
		t.Fatalf("tracker = %d orders / %s", orders, notional)
	}
}

func jsonDecode(r *http.Request, out *map[string]any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(out)
}

func TestPerOrderCapRejectsBeforeNetwork(t *testing.T) {
	client, arm, tracker := armedSetup(t)
	_ = client
	if err := arm.Arm("I_UNDERSTAND_THIS_TRADES_REAL_MONEY"); err != nil {
		t.Fatalf("arm: %v", err)
	}
	// Default per-order cap is $25; a $30 order must be refused with zero
	// network traffic (no fixture server even attached).
	req := PlaceRequest{
		Ticker: "T", ClientOrderID: "big", Side: "bid", CountFP: "60.00",
		PriceDollars: "0.50", TimeInForce: "good_till_canceled", SelfTradePreventionType: "maker",
	}
	_, err := client.PlaceOrder(context.Background(), req, arm, nil, tracker)
	if Code(err) != ErrLimitExceed {
		t.Fatalf("got %q (%v)", Code(err), err)
	}
	orders, _ := tracker.Snapshot()
	if orders != 0 {
		t.Fatal("rejected order must not be recorded")
	}
}

func TestDailyNotionalCapAccumulates(t *testing.T) {
	key := testKey(t)
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"order_id":"x","fill_count":"0.00","remaining_count":"40.00","ts_ms":1}`))
	})
	arm := &ArmState{}
	if err := arm.Arm("I_UNDERSTAND_THIS_TRADES_REAL_MONEY"); err != nil {
		t.Fatalf("arm: %v", err)
	}
	// Custom tracker: $50/order, $100/day — so each $40 order passes the
	// per-order cap but the third breaches the daily aggregate.
	tracker, err := NewRiskTracker(RiskLimits{
		MaxOrderNotionalDollars: "50.00",
		MaxDailyNotionalDollars: "100.00",
		MaxDailyOrders:          200,
	})
	if err != nil {
		t.Fatalf("tracker: %v", err)
	}
	mk := func(coid string) PlaceRequest {
		return PlaceRequest{Ticker: "T", ClientOrderID: coid, Side: "bid", CountFP: "80.00",
			PriceDollars: "0.50", TimeInForce: "good_till_canceled", SelfTradePreventionType: "maker"}
	}
	// $40 + $40 = $80 fits under the $100 daily cap...
	for _, coid := range []string{"a", "b"} {
		if _, err := client.PlaceOrder(context.Background(), mk(coid), arm, nil, tracker); err != nil {
			t.Fatalf("%s: %v", coid, err)
		}
	}
	// ...but a third $40 order would hit $120 > $100.
	_, err = client.PlaceOrder(context.Background(), mk("c"), arm, nil, tracker)
	if Code(err) != ErrLimitExceed {
		t.Fatalf("got %q (%v)", Code(err), err)
	}
	orders, notional := tracker.Snapshot()
	if orders != 2 || notional != "80.00" {
		t.Fatalf("tracker = %d / %s", orders, notional)
	}
}

func TestPlaceTimeoutReconcilesByClientOrderID(t *testing.T) {
	key := testKey(t)
	var posts atomic.Int32
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Query().Get("client_order_id") == "c-timeout":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"orders":[{"order_id":"found-1","client_order_id":"c-timeout","ticker":"T","status":"resting","yes_price_dollars":"0.50","fill_count_fp":"0.00","remaining_count_fp":"3.00","initial_count_fp":"3.00"}],"cursor":""}`))
		case r.Method == http.MethodPost:
			if posts.Add(1) > 1 {
				t.Error("more than one POST issued — that would be a blind retry")
			}
			time.Sleep(2 * time.Second) // simulate lost response; test ctx expires first
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	_, arm, tracker := armedSetup(t)
	if err := arm.Arm("I_UNDERSTAND_THIS_TRADES_REAL_MONEY"); err != nil {
		t.Fatalf("arm: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := client.PlaceOrder(ctx, PlaceRequest{
		Ticker: "T", ClientOrderID: "c-timeout", Side: "bid", CountFP: "3.00",
		PriceDollars: "0.50", TimeInForce: "good_till_canceled", SelfTradePreventionType: "maker",
	}, arm, nil, tracker)
	if err != nil {
		t.Fatalf("order landed despite timeout; reconciliation should find it, got %v", err)
	}
	if res.OrderID != "found-1" || res.StatusEcho != "resting" {
		t.Fatalf("reconciled result wrong: %+v", res)
	}
}

func TestAmendRequiresRestingStateAndArm(t *testing.T) {
	key := testKey(t)
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orderBody("executed")))
	})
	_, arm, tracker := armedSetup(t)
	if err := arm.Arm("I_UNDERSTAND_THIS_TRADES_REAL_MONEY"); err != nil {
		t.Fatalf("arm: %v", err)
	}
	req := PlaceRequest{Ticker: "T", Side: "bid", CountFP: "5.00", PriceDollars: "0.55"}
	_, err := client.AmendOrder(context.Background(), "ord1", req, arm, tracker)
	if Code(err) != ErrNotResting {
		t.Fatalf("amend executed order: got %q (%v)", Code(err), err)
	}

	// Un-armed: refused before any network call (client above has no server
	// for amend; if we reach the network this test fails).
	armEmpty := &ArmState{}
	_, err = client.AmendOrder(context.Background(), "ord1", req, armEmpty, tracker)
	if Code(err) != ErrDisarmed {
		t.Fatalf("unarmed amend: got %q (%v)", Code(err), err)
	}
}
