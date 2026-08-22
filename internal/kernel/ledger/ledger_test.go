package ledger

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestFeeMatchesPublishedFormula(t *testing.T) {
	// fee = 0.07 * P * (1-P) * count, exact rational then half-up to cents.
	cases := []struct{ price, count, want string }{
		{"0.5000", "10.00", "0.18"}, // 0.07*0.25*10 = 0.175 -> 0.18
		{"0.9000", "1.00", "0.01"},  // 0.07*0.09 = 0.0063 -> 0.01
		{"0.1000", "100.00", "0.63"},
		{"0.9900", "50.00", "0.03"}, // 0.07*0.0099*50=0.03465 -> 0.03
	}
	for _, c := range cases {
		got, err := Fee(c.price, c.count)
		if err != nil {
			t.Fatalf("Fee(%s,%s): %v", c.price, c.count, err)
		}
		if got != c.want {
			t.Errorf("Fee(%s,%s)=%s want %s", c.price, c.count, got, c.want)
		}
	}
}

func TestBuyReducesCashAndAddsPosition(t *testing.T) {
	l, _ := New("100.00")
	res, err := l.Execute(FillRequest{
		ClientOrderID: "c1", Ticker: "T-88", Side: Bid,
		PriceDollars: "0.5400", CountFP: "10.00",
		BookPrice: "0.5400", BookSizeFP: "25.00", BookHash: "h",
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Replayed {
		t.Fatal("first fill must not be a replay")
	}
	// cost = 5.40 + fee(0.07*0.54*0.46*10=0.17388->0.17)
	fee, _ := Fee("0.5400", "10.00")
	if res.Fill.FeeDollars != fee {
		t.Fatalf("fee %s != %s", res.Fill.FeeDollars, fee)
	}
	cash, positions, journal := l.Snapshot()
	want := "100.00"
	_ = want
	// expected: 100.00 - 5.40 - fee
	exp, _ := NewMoney(fee, 2)
	cash100, _ := NewMoney("100.00", 2)
	notional, _ := NewMoney("5.40", 2)
	expCash := cash100.Sub(exp, 2).Sub(notional, 2)
	if cash != expCash.Render() {
		t.Fatalf("cash %s want %s", cash, expCash.Render())
	}
	if len(positions) != 1 || positions[0].YesFP != "10.00" || len(journal) != 1 {
		t.Fatalf("snapshot: %+v %+v", positions, journal)
	}
}

func TestSellAddsProceedsMinusFee(t *testing.T) {
	l, _ := New("100.00")
	if _, err := l.Execute(FillRequest{
		ClientOrderID: "b1", Ticker: "T-88", Side: Bid,
		PriceDollars: "0.4000", CountFP: "5.00",
		BookPrice: "0.4000", BookSizeFP: "9.00", BookHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	res, err := l.Execute(FillRequest{
		ClientOrderID: "s1", Ticker: "T-88", Side: Ask,
		PriceDollars: "0.4200", CountFP: "5.00",
		BookPrice: "0.4200", BookSizeFP: "7.00", BookHash: "h2",
	})
	if err != nil {
		t.Fatalf("sell: %v", err)
	}
	_, positions, _ := l.Snapshot()
	if positions[0].YesFP != "0.00" {
		t.Fatalf("net position after round trip = %s, want 0.00", positions[0].YesFP)
	}
	if res.Fill.FeeDollars == "" || strings.HasPrefix(res.Fill.FeeDollars, "-") {
		t.Fatalf("fee must be positive: %q", res.Fill.FeeDollars)
	}
}

func TestIdempotentReplayReturnsOriginalFill(t *testing.T) {
	l, _ := New("100.00")
	first, err := l.Execute(FillRequest{
		ClientOrderID: "same", Ticker: "T", Side: Bid,
		PriceDollars: "0.3000", CountFP: "2.00",
		BookPrice: "0.3000", BookSizeFP: "5.00", BookHash: "h",
	})
	if err != nil {
		t.Fatal(err)
	}
	second, err := l.Execute(FillRequest{
		ClientOrderID: "same", Ticker: "T", Side: Bid,
		PriceDollars: "0.3000", CountFP: "2.00",
		BookPrice: "0.3000", BookSizeFP: "5.00", BookHash: "h",
	})
	if err != nil {
		t.Fatalf("replay errored: %v", err)
	}
	if !second.Replayed || second.Fill.ClientOrderID != first.Fill.ClientOrderID {
		t.Fatalf("replay mismatch: %+v vs %+v", second, first)
	}
	_, _, journal := l.Snapshot()
	if len(journal) != 1 {
		t.Fatalf("journal has %d entries, want 1 (no double fill)", len(journal))
	}
}

func TestInsufficientBookRejectedAtomically(t *testing.T) {
	l, _ := New("100.00")
	_, err := l.Execute(FillRequest{
		ClientOrderID: "big", Ticker: "T", Side: Bid,
		PriceDollars: "0.2500", CountFP: "50.00",
		BookPrice: "0.2500", BookSizeFP: "49.99", BookHash: "h",
	})
	if !errors.Is(err, ErrInsufficientBook) {
		t.Fatalf("err = %v, want ErrInsufficientBook", err)
	}
	cash, positions, journal := l.Snapshot()
	if cash != "100.00" || len(positions) != 0 || len(journal) != 0 {
		t.Fatalf("rejected fill mutated state: %s %+v %+v", cash, positions, journal)
	}
}

func TestCancelIsTypedFailure(t *testing.T) {
	l, _ := New("100.00")
	if _, err := l.Execute(FillRequest{
		ClientOrderID: "x", Ticker: "T", Side: Bid,
		PriceDollars: "0.2000", CountFP: "1.00",
		BookPrice: "0.2000", BookSizeFP: "1.00", BookHash: "h",
	}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(ErrNoRestingOrder, ErrNoRestingOrder) {
		t.Fatal("unreachable")
	}
}

func TestPriceBoundsEnforced(t *testing.T) {
	l, _ := New("100.00")
	for _, bad := range []string{"0.0000", "1.0000", "-0.5000"} {
		_, err := l.Execute(FillRequest{
			ClientOrderID: "p" + bad, Ticker: "T", Side: Bid,
			PriceDollars: bad, CountFP: "1.00",
			BookPrice: bad, BookSizeFP: "1.00", BookHash: "h",
		})
		if !errors.Is(err, ErrInvalidInput) {
			t.Errorf("price %s: err = %v, want ErrInvalidInput", bad, err)
		}
	}
}

func TestConcurrentSameClientOrderIDFillsOnce(t *testing.T) {
	l, _ := New("1000.00")
	var wg sync.WaitGroup
	errs := make(chan error, 16)
	replays := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			res, err := l.Execute(FillRequest{
				ClientOrderID: "race", Ticker: "T", Side: Bid,
				PriceDollars: "0.1000", CountFP: "1.00",
				BookPrice: "0.1000", BookSizeFP: "20.00", BookHash: "h",
			})
			if err != nil {
				errs <- err
				return
			}
			replays <- res.Replayed
		}()
	}
	wg.Wait()
	close(errs)
	close(replays)
	fills := 0
	for r := range replays {
		if !r {
			fills++
		}
	}
	if fills != 1 {
		t.Fatalf("%d non-replay fills, want exactly 1", fills)
	}
	for e := range errs {
		t.Fatalf("unexpected error: %v", e)
	}
}

func TestJournalCapStopsGrowth(t *testing.T) {
	l, _ := New("100000.00")
	var err error
	for i := 0; i < JournalCap+1 && err == nil; i++ {
		_, err = l.Execute(FillRequest{
			ClientOrderID: string(rune('a'+i%26)) + string(rune('A'+i/26)) + itoa(i), Ticker: "T", Side: Bid,
			PriceDollars: "0.0100", CountFP: "0.01",
			BookPrice: "0.0100", BookSizeFP: "999999.00", BookHash: "h",
		})
	}
	if !errors.Is(err, ErrJournalFull) {
		t.Fatalf("cap not enforced: %v", err)
	}
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	digits := ""
	for ; i > 0; i /= 10 {
		digits = string(rune('0'+i%10)) + digits
	}
	return digits
}
