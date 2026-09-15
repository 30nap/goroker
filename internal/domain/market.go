package domain

import "time"

// MarketState is the trading state of the exchange as reported by the broker UI.
type MarketState string

const (
	// MarketOpen means the broker UI positively reports an open, tradable market.
	MarketOpen MarketState = "OPEN"
	// MarketClosed means the broker UI positively reports a closed market.
	MarketClosed MarketState = "CLOSED"
	// MarketUnknown means the state could not be established. It is NEVER
	// treated as open.
	MarketUnknown MarketState = "UNKNOWN"
)

// MarketStatus is a timestamped observation of the market state.
type MarketStatus struct {
	State MarketState
	// Raw is the untranslated label read from the broker UI, kept for logs
	// and troubleshooting. It must never influence trading decisions.
	Raw string
	// ObservedAt is when the state was read from the UI.
	ObservedAt time.Time
}

// IsOpen reports whether trading is positively allowed. UNKNOWN is never open.
func (m MarketStatus) IsOpen() bool { return m.State == MarketOpen }

// UnknownMarket returns a MarketStatus that always fails the open check.
func UnknownMarket(raw string, at time.Time) MarketStatus {
	return MarketStatus{State: MarketUnknown, Raw: raw, ObservedAt: at}
}
