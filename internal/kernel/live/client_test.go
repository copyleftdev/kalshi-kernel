package live

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"
)

func testKey(t *testing.T) *rsa.PrivateKey {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("keygen: %v", err)
	}
	return key
}

// verifySignature re-derives the signed payload server-side and checks the
// RSA-PSS signature, proving the header scheme matches Kalshi's contract.
func verifySignature(t *testing.T, key *rsa.PrivateKey, r *http.Request) {
	t.Helper()
	tsMs := r.Header.Get("KALSHI-ACCESS-TIMESTAMP")
	sig := r.Header.Get("KALSHI-ACCESS-SIGNATURE")
	keyID := r.Header.Get("KALSHI-ACCESS-KEY")
	if tsMs == "" || sig == "" || keyID == "" || keyID != "test-key" {
		t.Errorf("missing or wrong auth headers: key=%q", keyID)
	}
	ts, err := strconv.ParseInt(tsMs, 10, 64)
	if err != nil {
		t.Fatalf("timestamp not ms int: %v", err)
	}
	if delta := time.Now().UnixMilli() - ts; delta < -60000 || delta > 60000 {
		t.Errorf("timestamp drift %dms too large", delta)
	}
	base := tsMs + r.Method + r.URL.RequestURI()
	digest := sha256.Sum256([]byte(base))
	raw, err := base64.StdEncoding.DecodeString(sig)
	if err != nil {
		t.Fatalf("signature not base64: %v", err)
	}
	if err := rsa.VerifyPSS(&key.PublicKey, crypto.SHA256, digest[:], raw, &rsa.PSSOptions{SaltLength: rsa.PSSSaltLengthEqualsHash}); err != nil {
		t.Fatalf("RSA-PSS signature verification failed: %v", err)
	}
}

func TestGetPortfolioSignsAndProjects(t *testing.T) {
	key := testKey(t)
	var calls []string
	client := NewWithBaseURL("test-key", key, "", nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		verifySignature(t, key, r)
		calls = append(calls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/trade-api/v2/portfolio/get_balance":
			_, _ = w.Write([]byte(`{"balance":10000,"balance_dollars":"100.00","portfolio_value":250,"updated_ts":1700000000}`))
		case "/trade-api/v2/portfolio/get_positions":
			_, _ = w.Write([]byte(`{"market_positions":[{"ticker":"KXHIGHNY-26AUG23-T87","position_fp":"5.00","market_exposure_dollars":"0.05","realized_pnl_dollars":"0.00","fees_paid_dollars":"0.09","last_updated_ts":"2026-08-22T18:00:00Z"}]}`))
		case "/trade-api/v2/portfolio/get_orders":
			_, _ = w.Write([]byte(`{"orders":[{"order_id":"ord1","client_order_id":"c1","ticker":"KXHIGHNY-26AUG23-T87","side":"yes","action":"buy","count_fp":"5.00","yes_price_dollars":"0.0100","status":"executed","filled_count_fp":"5.00","remaining_count_fp":"0.00","created_time":"2026-08-22T17:00:00Z"}]}`))
		case "/trade-api/v2/portfolio/get_fills":
			_, _ = w.Write([]byte(`{"fills":[{"fill_id":"f1","order_id":"ord1","ticker":"KXHIGHNY-26AUG23-T87","outcome_side":"yes","book_side":"bid","count_fp":"5.00","yes_price_dollars":"0.0100","is_taker":true,"fee_cost":"0.09","created_time":"2026-08-22T17:00:01Z"}],"cursor":""}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client.baseURL = server.URL
	client.http = server.Client()

	p, err := client.GetPortfolio(context.Background())
	if err != nil {
		t.Fatalf("GetPortfolio: %v", err)
	}
	wantPaths := []string{
		"/trade-api/v2/portfolio/get_balance",
		"/trade-api/v2/portfolio/get_positions",
		"/trade-api/v2/portfolio/get_orders",
		"/trade-api/v2/portfolio/get_fills",
	}
	if len(calls) != 4 {
		t.Fatalf("calls = %v", calls)
	}
	for i, want := range wantPaths {
		if calls[i] != want {
			t.Errorf("call %d = %s, want %s", i, calls[i], want)
		}
	}
	// Fixed-point passthrough byte-for-byte.
	if p.Balance.BalanceDollars != "100.00" || p.Balance.PortfolioDollars != "2.50" {
		t.Errorf("balance wrong: %+v", p.Balance)
	}
	if len(p.Positions) != 1 || p.Positions[0].PositionFP != "5.00" || p.Positions[0].FeesPaidDollars != "0.09" {
		t.Errorf("positions wrong: %+v", p.Positions)
	}
	if len(p.Orders) != 1 || p.Orders[0].OrderID != "ord1" || p.Orders[0].RemainingFP != "0.00" {
		t.Errorf("orders wrong: %+v", p.Orders)
	}
	if len(p.Fills) != 1 || p.Fills[0].YesPrice != "0.0100" || p.Fills[0].FeeCost != "0.09" {
		t.Errorf("fills wrong: %+v", p.Fills)
	}
	out, _ := json.Marshal(p)
	if json.Valid(out) != true {
		t.Fatal("projection is not valid JSON")
	}
}

func TestUnauthorizedIsTypedNotLeaky(t *testing.T) {
	key := testKey(t)
	client := NewWithBaseURL("test-key", key, "", nil)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"message":"bad signature for user x","code":"auth_error"}`))
	}))
	defer server.Close()
	client.baseURL = server.URL
	client.http = server.Client()

	_, err := client.GetPortfolio(context.Background())
	if Code(err) != ErrUnauthed {
		t.Fatalf("code = %q (%v)", Code(err), err)
	}
	// The upstream body names the user; our error must NOT echo it.
	if contains(err.Error(), "user x") {
		t.Fatalf("error leaked upstream body: %v", err)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func TestRateLimitedAndUpstreamTyped(t *testing.T) {
	key := testKey(t)
	statuses := []int{http.StatusTooManyRequests, http.StatusInternalServerError}
	for _, status := range statuses {
		client := NewWithBaseURL("test-key", key, "", nil)
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		client.baseURL = server.URL
		client.http = server.Client()
		_, err := client.GetPortfolio(context.Background())
		want := ErrRateLimited
		if status == http.StatusInternalServerError {
			want = ErrUpstream
		}
		if Code(err) != want {
			t.Errorf("status %d: code = %q, want %q", status, Code(err), want)
		}
		server.Close()
	}
}

func TestBadKeyMaterialIsRejectedBeforeNetwork(t *testing.T) {
	dir := t.TempDir()
	bad := dir + "/key.pem"
	if err := writeFile(bad, "not a pem"); err == nil {
		// fallthrough; New must fail below
	} else {
		t.Fatalf("write fixture: %v", err)
	}
	if _, err := New("k", bad); Code(err) != ErrBadKey {
		t.Fatalf("non-PEM: got %v", err)
	}
	if _, err := New("", ""); Code(err) != ErrBadInput {
		t.Fatalf("empty config: got %v", err)
	}
	if _, err := New("k", dir+"/missing.pem"); Code(err) != ErrBadKey {
		t.Fatalf("missing file: got %v", err)
	}
}

func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
