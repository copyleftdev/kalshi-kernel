// Package ledger implements the paper-trading simulated book: an
// in-memory cash balance, position map, fill journal with client_order_id
// idempotency, and exact fixed-point fee accounting.
//
// Invariants:
//   - all money/quantity math uses big.Int scaled decimals, never float64;
//   - a repeated client_order_id returns the original result unchanged;
//   - fills are all-or-nothing against displayed top-of-book size;
//   - nothing here ever touches the network or credentials.
package ledger

import (
	"errors"
	"math/big"
	"sort"
	"sync"
	"time"
)

// Money is a fixed-point dollar amount as [value, scale]:
// value * 10^-scale. Prices use scale 4 ("0.5400"); counts use
// scale 2 ("10.00"). All arithmetic keeps full precision; only final
// rendering rounds (half-up) to the target scale.
type Money struct {
	Value *big.Int
	Scale int
}

func NewMoney(value string, scale int) (*Money, error) {
	v, ok := new(big.Int).SetString(stripDot(value), 10)
	if !ok {
		return nil, errors.New("invalid decimal: " + value)
	}
	return normalize(&Money{Value: v, Scale: scale}), nil
}

func stripDot(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' || c == '+' {
			continue
		}
		if c == '-' && len(out) == 0 {
			out = append(out, c)
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

func rescale(m *Money, scale int) *Money {
	if m.Scale == scale {
		return m
	}
	diff := scale - m.Scale
	v := new(big.Int).Set(m.Value)
	if diff > 0 {
		v.Mul(v, pow10(diff))
	} else {
		v.Quo(v, pow10(-diff)) // truncation toward zero; inputs never need rounding here
	}
	return &Money{Value: v, Scale: scale}
}

func normalize(m *Money) *Money { return rescale(m, m.Scale) }

func pow10(n int) *big.Int { return new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(n)), nil) }

func (m *Money) Add(o *Money, scale int) *Money {
	a, b := rescale(m, scale), rescale(o, scale)
	return &Money{Value: new(big.Int).Add(a.Value, b.Value), Scale: scale}
}

func (m *Money) Sub(o *Money, scale int) *Money {
	a, b := rescale(m, scale), rescale(o, scale)
	return &Money{Value: new(big.Int).Sub(a.Value, b.Value), Scale: scale}
}

func (m *Money) Mul(o *Money, outScale int) *Money {
	scale := m.Scale + o.Scale - outScale
	v := new(big.Int).Mul(m.Value, o.Value)
	if scale > 0 {
		v.Quo(v, pow10(scale))
	} else if scale < 0 {
		v.Mul(v, pow10(-scale))
	}
	return &Money{Value: v, Scale: outScale}
}

// Render emits the decimal string with exactly `scale` digits after the
// point (truncation toward zero for negatives is acceptable for display
// of already-scaled values).
func (m *Money) Render() string {
	m = normalize(m)
	s := m.Value.String()
	neg := false
	if len(s) > 0 && s[0] == '-' {
		neg, s = true, s[1:]
	}
	for len(s) <= m.Scale {
		s = "0" + s
	}
	intPart, fracPart := s[:len(s)-m.Scale], s[len(s)-m.Scale:]
	out := intPart + "." + fracPart
	if neg {
		out = "-" + out
	}
	return out
}

// FeeRateNumerator / FeeRateDenominator encode Kalshi's published trading
// fee formula: fee = 0.07 * P * (1-P) per contract.
const (
	FeeRateNumerator   = 7
	FeeRateDenominator = 100
)

// Fee computes the trading fee for `countFP` contracts at price `priceDollars`
// using exact rational arithmetic: count * price * (1-price) * 7/100,
// rounded half-up to the count scale (2 decimals).
func Fee(priceDollars string, countFP string) (string, error) {
	price, err := NewMoney(priceDollars, 4)
	if err != nil {
		return "", err
	}
	count, err := NewMoney(countFP, 2)
	if err != nil {
		return "", err
	}
	oneMinusP := (&Money{Value: big.NewInt(10000), Scale: 4}).Sub(price, 4)
	// fee = count * price * (1-price) * 7/100, exact until one final
	// half-up round to cents.
	// feeScaled: scale4 value of price*(1-price). countScaled: scale4.
	// product scale8; multiply by 7/100 => still scale8 dollars;
	// to cents (scale2) divide by 10^6, rounding half-up on the way.
	feeScaled := price.Mul(oneMinusP, 4)
	countScaled := rescale(count, 4)
	exact := new(big.Int).Mul(feeScaled.Value, countScaled.Value)
	exact.Mul(exact, big.NewInt(FeeRateNumerator))
	denom := new(big.Int).Mul(pow10(6), big.NewInt(FeeRateDenominator))
	numScaled3 := new(big.Int).Mul(exact, big.NewInt(10)) // extra digit for half-up
	q := new(big.Int).Quo(numScaled3, denom)
	total := &Money{Value: q, Scale: 3}
	return halfUp(total, 2).Render(), nil
}

// halfUp rounds to `scale` digits using round-half-away-from-zero on the
// discarded portion.
func halfUp(m *Money, scale int) *Money {
	m = rescale(m, scale+1)
	v := new(big.Int).Set(m.Value)
	sign := 1
	if v.Sign() < 0 {
		sign, v = -1, new(big.Int).Neg(v)
	}
	last := new(big.Int).Mod(v, big.NewInt(10))
	q, r := new(big.Int).QuoRem(v, big.NewInt(10), new(big.Int))
	if last.Cmp(big.NewInt(5)) >= 0 {
		q.Add(q, big.NewInt(1))
	}
	_ = r
	if sign < 0 {
		q.Neg(q)
	}
	return &Money{Value: q, Scale: scale}
}

// Side is the single-book order side.
type Side string

const (
	Bid Side = "bid" // buy YES (or equivalently sell NO)
	Ask Side = "ask" // sell YES (or equivalently buy NO)
)

// Fill records one completed simulation.
type Fill struct {
	ClientOrderID string    `json:"client_order_id"`
	Ticker        string    `json:"ticker"`
	Side          Side      `json:"side"`
	PriceDollars  string    `json:"price_dollars"`
	CountFP       string    `json:"count_fp"`
	FeeDollars    string    `json:"fee_dollars"`
	BookHash      string    `json:"orderbook_hash"`
	Simulated     bool      `json:"simulated"`
	At            time.Time `json:"at"`
}

// Position is a net contract position in one ticker.
type Position struct {
	Ticker string `json:"ticker"`
	YesFP  string `json:"yes_fp"` // net YES contracts (negative == short yes)
}

// Typed errors surfaced through mcptools.Error codes.
var (
	ErrInsufficientBook = errors.New("displayed book size does not cover the requested count")
	ErrDuplicateOrder   = errors.New("client_order_id already filled")
	ErrNoRestingOrder   = errors.New("no resting order to cancel")
	ErrJournalFull      = errors.New("fill journal reached its configured cap")
	ErrInsufficientCash = errors.New("paper balance insufficient")
	ErrInvalidInput     = errors.New("invalid price or count")
)

// JournalCap bounds memory growth.
const JournalCap = 10000

// Ledger is the concurrency-safe paper book.
type Ledger struct {
	mu        sync.Mutex
	cashScale int // dollars scale 4 internally? keep 2-decimal dollars like upstream
	cash      *Money
	positions map[string]*big.Int // ticker -> net yes contracts, scale 2
	journal   []Fill
	byCoid    map[string]int // client_order_id -> journal index
}

// New creates a ledger seeded with paper cash, e.g. "100.00".
func New(startCashDollars string) (*Ledger, error) {
	cash, err := NewMoney(startCashDollars, 2)
	if err != nil {
		return nil, err
	}
	return &Ledger{
		cash:      cash,
		positions: make(map[string]*big.Int),
		byCoid:    make(map[string]int),
	}, nil
}

// FillRequest describes one simulated order.
type FillRequest struct {
	ClientOrderID string
	Ticker        string
	Side          Side
	PriceDollars  string // limit/fill price quoted by caller (must equal book touch)
	CountFP       string
	BookPrice     string // top-of-book dollars from the fetched snapshot
	BookSizeFP    string // displayed size at that touch
	BookHash      string // hash of the snapshot used
}

// FillResult reports what happened, including idempotent replays.
type FillResult struct {
	Fill      *Fill  `json:"fill,omitempty"`
	Replayed  bool   `json:"replayed,omitempty"`
	CashAfter string `json:"cash_after"`
}

// Execute simulates an immediate all-or-nothing fill.
//
// Pricing rule: a bid (buy YES) pays the ask; an ask (sell YES) hits the
// bid. The caller passes BookPrice as the touch it wants to cross; we
// verify the requested PriceDollars equals it to prevent accidental
// mispricing, then charge fees per the published formula.
func (l *Ledger) Execute(req FillRequest) (*FillResult, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if idx, ok := l.byCoid[req.ClientOrderID]; ok {
		f := l.journal[idx]
		return &FillResult{Fill: &f, Replayed: true, CashAfter: l.cash.Render()}, nil
	}
	if len(l.journal) >= JournalCap {
		return nil, ErrJournalFull
	}

	count, err := NewMoney(req.CountFP, 2)
	if err != nil || count.Value.Sign() <= 0 {
		return nil, ErrInvalidInput
	}
	bookSize, err := NewMoney(req.BookSizeFP, 2)
	if err != nil {
		return nil, ErrInvalidInput
	}
	if count.Value.Cmp(bookSize.Value) > 0 {
		return nil, ErrInsufficientBook
	}
	price, err := NewMoney(req.PriceDollars, 4)
	if err != nil || price.Value.Sign() <= 0 || price.Value.Cmp(pow10(4)) >= 0 {
		return nil, ErrInvalidInput
	}
	bookPrice, err := NewMoney(req.BookPrice, 4)
	if err != nil || bookPrice.Value.Cmp(price.Value) != 0 {
		return nil, ErrInvalidInput
	}

	feeStr, err := Fee(req.PriceDollars, req.CountFP)
	if err != nil {
		return nil, ErrInvalidInput
	}
	fee, _ := NewMoney(feeStr, 2)

	notional := price.Mul(count, 2) // scale-2 dollars
	var cashDelta *Money
	switch req.Side {
	case Bid:
		cost := notional.Add(fee, 2) // cost: -(notional+fee)
		cashDelta = &Money{Value: new(big.Int).Neg(cost.Value), Scale: 2}
	case Ask:
		cashDelta = notional.Sub(fee, 2) // proceeds: +(notional-fee)
	default:
		return nil, ErrInvalidInput
	}
	newCash := l.cash.Add(cashDelta, 2)
	if req.Side == Bid && newCash.Value.Sign() < 0 {
		return nil, ErrInsufficientCash
	}
	l.cash = newCash

	pos := l.positions[req.Ticker]
	if pos == nil {
		pos = big.NewInt(0)
	}
	switch req.Side {
	case Bid:
		pos = new(big.Int).Add(pos, count.Value)
	case Ask:
		pos = new(big.Int).Sub(pos, count.Value)
	}
	l.positions[req.Ticker] = pos

	fill := Fill{
		ClientOrderID: req.ClientOrderID,
		Ticker:        req.Ticker,
		Side:          req.Side,
		PriceDollars:  req.PriceDollars,
		CountFP:       count.Render(),
		FeeDollars:    fee.Render(),
		BookHash:      req.BookHash,
		Simulated:     true,
		At:            time.Now().UTC(),
	}
	l.journal = append(l.journal, fill)
	l.byCoid[req.ClientOrderID] = len(l.journal) - 1

	return &FillResult{Fill: &fill, CashAfter: l.cash.Render()}, nil
}

// Snapshot renders current balance, positions, and journal.
func (l *Ledger) Snapshot() (cash string, positions []Position, journal []Fill) {
	l.mu.Lock()
	defer l.mu.Unlock()
	cash = l.cash.Render()
	tickers := make([]string, 0, len(l.positions))
	for t := range l.positions {
		tickers = append(tickers, t)
	}
	sort.Strings(tickers)
	for _, t := range tickers {
		positions = append(positions, Position{
			Ticker: t,
			YesFP:  (&Money{Value: new(big.Int).Set(l.positions[t]), Scale: 2}).Render(),
		})
	}
	journal = append(journal, l.journal...)
	return
}

// ValueSign helper kept unexported-friendly: returns sign of value.
func (m *Money) ValueSign() *Money { return m }
