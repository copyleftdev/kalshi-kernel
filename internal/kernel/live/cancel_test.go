package live

import (
	"context"
	"crypto/rsa"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, key *rsa.PrivateKey, handler http.HandlerFunc) *Client {
	t.Helper()
	client := NewWithBaseURL("test-key", key, "", nil)
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	client.baseURL = server.URL
	client.http = server.Client()
	return client
}

func orderBody(status string) string {
	return `{"order":{"order_id":"ord1","client_order_id":"c1","ticker":"KXHIGHNY-26AUG23-T87","outcome_side":"yes","book_side":"bid","status":"` + status + `","type":"limit","yes_price_dollars":"0.5500","fill_count_fp":"0.00","remaining_count_fp":"5.00","initial_count_fp":"5.00"}}`
}

func TestCancelRestingOrderHappyPath(t *testing.T) {
	key := testKey(t)
	var sawDelete atomic.Bool
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		verifySignature(t, key, r)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/trade-api/v2/portfolio/orders/ord1":
			_, _ = w.Write([]byte(orderBody("resting")))
		case r.Method == http.MethodDelete && r.URL.Path == "/trade-api/v2/portfolio/events/orders/ord1":
			if r.URL.Query().Get("market_ticker") != "KXHIGHNY-26AUG23-T87" {
				t.Errorf("market_ticker not forwarded for auto-routing: %v", r.URL.Query())
			}
			sawDelete.Store(true)
			_, _ = w.Write([]byte(`{"order_id":"ord1","client_order_id":"c1","reduced_by":"5.00","ts_ms":1715793660456}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
	})
	res, err := client.CancelOrder(context.Background(), "ord1", "KXHIGHNY-26AUG23-T87")
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if !sawDelete.Load() {
		t.Fatal("DELETE never issued")
	}
	if res.OrderID != "ord1" || res.ReducedByFP != "5.00" || res.TsMs != 1715793660456 {
		t.Fatalf("result wrong: %+v", res)
	}
}

func TestCancelStateGateRejectsNonResting(t *testing.T) {
	key := testKey(t)
	cases := map[string]string{
		"canceled": ErrNotResting,
		"executed": ErrNotResting,
	}
	for status, want := range cases {
		client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(orderBody(status)))
		})
		_, err := client.CancelOrder(context.Background(), "ord1", "T")
		if Code(err) != want {
			t.Errorf("status %s: code = %q (%v), want %q", status, Code(err), err, want)
		}
	}
}

func TestCancelMissingOrderIsTyped(t *testing.T) {
	key := testKey(t)
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := client.CancelOrder(context.Background(), "nope", "T")
	if Code(err) != ErrUpstream {
		t.Fatalf("got %q (%v)", Code(err), err)
	}
}

// The core stage-2 guarantee: a DELETE that times out must NEVER be
// reported as a clean failure. The client must re-query and report the
// truth — either the cancel landed, the order still rests, or an
// explicit outcome_indeterminate.
func TestCancelTimeoutThenReconcileCanceled(t *testing.T) {
	key := testKey(t)
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/trade-api/v2/portfolio/orders/ord1":
			// First GET (state gate): resting. Second GET (reconcile): canceled.
			if atomic.LoadInt32(&getCount) == 0 {
				atomic.AddInt32(&getCount, 1)
				_, _ = w.Write([]byte(orderBody("resting")))
				return
			}
			_, _ = w.Write([]byte(orderBody("canceled")))
		case r.Method == http.MethodDelete:
			// Simulate the request reaching the exchange but the response
			// never coming back: hang until the client's context expires.
			<-r.Context().Done()
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	res, err := client.CancelOrder(ctx, "ord1", "T")
	if err != nil {
		t.Fatalf("cancel landed despite timeout; should reconcile to success, got %v", err)
	}
	if res.OrderID != "ord1" || res.ReducedByFP != "5.00" {
		t.Fatalf("reconciled result wrong: %+v", res)
	}
}

var getCount int32

func TestCancelTimeoutThenStillRestingIsIndeterminate(t *testing.T) {
	key := testKey(t)
	var gets atomic.Int32
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodDelete {
			<-r.Context().Done()
			return
		}
		// Every GET returns resting: cancel never landed.
		gets.Add(1)
		_, _ = w.Write([]byte(orderBody("resting")))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := client.CancelOrder(ctx, "ord1", "T")
	if Code(err) != ErrIndeterminate {
		t.Fatalf("code = %q (%v), want outcome_indeterminate", Code(err), err)
	}
	if gets.Load() < 2 {
		t.Fatalf("expected state-gate GET + reconcile GET, got %d", gets.Load())
	}
}

func TestCancelTimeoutAndQueryFailsIsIndeterminate(t *testing.T) {
	key := testKey(t)
	var gets atomic.Int32
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			<-r.Context().Done()
			return
		}
		// State gate OK, but the reconciliation GET 500s.
		if gets.Add(1) == 1 {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(orderBody("resting")))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := client.CancelOrder(ctx, "ord1", "T")
	if Code(err) != ErrIndeterminate {
		t.Fatalf("code = %q (%v), want outcome_indeterminate", Code(err), err)
	}
}

func TestGetOrderProjectsAuthoritativeState(t *testing.T) {
	key := testKey(t)
	client := newTestClient(t, key, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/trade-api/v2/portfolio/orders/ord1" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(orderBody("resting")))
	})
	o, err := client.GetOrder(context.Background(), "ord1")
	if err != nil {
		t.Fatalf("GetOrder: %v", err)
	}
	if o.Status != "resting" || o.YesPrice != "0.5500" || o.RemainingFP != "5.00" || o.InitialCountFP != "5.00" {
		t.Fatalf("projection wrong: %+v", o)
	}
}
