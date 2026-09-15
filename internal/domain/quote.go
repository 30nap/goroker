package domain

import (
	"fmt"
	"time"
)

// Symbol is an exactly resolved broker instrument.
type Symbol struct {
	// Name is the symbol as displayed by the broker (e.g. "فولاد").
	Name string
	// ISIN is the instrument identifier when the broker exposes one.
	ISIN string
	// BrokerID is the broker's internal instrument key, used to build URLs
	// or to select the instrument in the order form.
	BrokerID string
	// Title is the full company name when available.
	Title string
}

// Quote is a timestamped price observation. All prices are integer Rial/Toman
// amounts exactly as displayed by the broker; no floating point is used for money.
type Quote struct {
	Symbol    string
	LastPrice int64
	BestAsk   int64
	BestBid   int64
	// Timestamp is the local observation time, used for freshness checks.
	Timestamp time.Time
}

// Age returns how long ago the quote was observed relative to now.
func (q Quote) Age(now time.Time) time.Duration { return now.Sub(q.Timestamp) }

// IsFresh reports whether the quote was observed within maxAge. A zero
// timestamp is never fresh, and a timestamp in the future is rejected as well:
// freshness that cannot be established must abort.
func (q Quote) IsFresh(now time.Time, maxAge time.Duration) bool {
	if q.Timestamp.IsZero() || maxAge <= 0 {
		return false
	}
	age := q.Age(now)
	if age < 0 {
		return false
	}
	return age <= maxAge
}

// PriceSource names the quote field a trading decision is based on. Goroker
// never silently switches between price definitions; the source is explicit
// and is carried through to the confirmation prompt and logs.
type PriceSource string

const (
	// PriceSourceBestAsk is the preferred source for BUY preparation.
	PriceSourceBestAsk PriceSource = "BEST_ASK"
	// PriceSourceExplicit is used when the user passes --price.
	PriceSourceExplicit PriceSource = "EXPLICIT"
)

// Price returns the quote field named by src.
func (q Quote) Price(src PriceSource) (int64, error) {
	switch src {
	case PriceSourceBestAsk:
		return q.BestAsk, nil
	default:
		return 0, fmt.Errorf("%w: quote has no field for price source %q", ErrQuoteInvalid, src)
	}
}

// Validate rejects quotes that cannot support a trading decision.
func (q Quote) Validate() error {
	if q.Symbol == "" {
		return fmt.Errorf("%w: empty symbol", ErrQuoteInvalid)
	}
	if q.Timestamp.IsZero() {
		return fmt.Errorf("%w: missing observation timestamp", ErrQuoteInvalid)
	}
	if q.LastPrice < 0 || q.BestAsk < 0 || q.BestBid < 0 {
		return fmt.Errorf("%w: negative price", ErrQuoteInvalid)
	}
	return nil
}
