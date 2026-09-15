package domain

import (
	"fmt"
	"time"
)

// OrderSide is the direction of an order. Goroker only ever prepares BUY.
type OrderSide string

const (
	SideBuy OrderSide = "BUY"
	// SideSell exists so that a SELL read back from the DOM can be detected
	// and rejected. Goroker never prepares or submits a SELL order.
	SideSell OrderSide = "SELL"
)

// PriceMode describes how the order price is chosen.
type PriceMode string

const (
	// PriceModeRange waits for the quote to enter [MinPrice, MaxPrice] and
	// uses the observed best ask as the order price.
	PriceModeRange PriceMode = "RANGE"
	// PriceModeExact uses exactly the price the user asked for and never
	// alters it.
	PriceModeExact PriceMode = "EXACT"
)

// BuyOrderRequest is the user's request, exactly as typed. Goroker never
// silently modifies any field of it.
type BuyOrderRequest struct {
	Symbol   string
	MinPrice int64
	MaxPrice int64
	Price    int64
	Quantity int64
}

// Mode reports whether this is an exact-price or a range request.
func (r BuyOrderRequest) Mode() PriceMode {
	if r.Price > 0 {
		return PriceModeExact
	}
	return PriceModeRange
}

// Validate checks the request for internal consistency before any browser
// interaction happens.
func (r BuyOrderRequest) Validate() error {
	if r.Symbol == "" {
		return fmt.Errorf("%w: symbol is required", ErrAbort)
	}
	if r.Quantity <= 0 {
		return fmt.Errorf("%w: quantity must be greater than zero", ErrAbort)
	}
	switch r.Mode() {
	case PriceModeExact:
		if r.MinPrice != 0 || r.MaxPrice != 0 {
			return fmt.Errorf("%w: --price cannot be combined with --min-price/--max-price", ErrAbort)
		}
		if r.Price <= 0 {
			return fmt.Errorf("%w: price must be greater than zero", ErrAbort)
		}
	case PriceModeRange:
		if r.MinPrice <= 0 || r.MaxPrice <= 0 {
			return fmt.Errorf("%w: --min-price and --max-price are required unless --price is given", ErrAbort)
		}
		if r.MinPrice > r.MaxPrice {
			return fmt.Errorf("%w: --min-price (%d) is greater than --max-price (%d)", ErrAbort, r.MinPrice, r.MaxPrice)
		}
	}
	return nil
}

// Bounds returns the inclusive price window the order price must fall inside.
// For exact-price requests the window is the single requested price, so every
// later validation step applies unchanged.
func (r BuyOrderRequest) Bounds() (min, max int64) {
	if r.Mode() == PriceModeExact {
		return r.Price, r.Price
	}
	return r.MinPrice, r.MaxPrice
}

// InRange reports whether price is inside the request's inclusive bounds.
func (r BuyOrderRequest) InRange(price int64) bool {
	min, max := r.Bounds()
	return price >= min && price <= max
}

// PreparedOrder is an order that exists in the broker's order form but has not
// been submitted. The same type is used for what Goroker intends to enter and
// for what it reads back from the DOM, so the two can be compared field by field.
type PreparedOrder struct {
	Symbol        string
	Side          OrderSide
	Price         int64
	Quantity      int64
	EstimatedCost int64
	// PriceSource records which quote field the price came from.
	PriceSource PriceSource
	// PreparedAt is when the form was filled or read back.
	PreparedAt time.Time
}

// ExpectedCost is Price*Quantity. The broker's own estimate is compared
// against it when the UI exposes one.
func (p PreparedOrder) ExpectedCost() int64 { return p.Price * p.Quantity }

// Matches compares the order read back from the DOM against the expected
// order. Any difference is an ABORT condition.
func (p PreparedOrder) Matches(other PreparedOrder) error {
	if p.Symbol != other.Symbol {
		return fmt.Errorf("%w: symbol expected %q, DOM has %q", ErrOrderMismatch, p.Symbol, other.Symbol)
	}
	if p.Side != other.Side {
		return fmt.Errorf("%w: side expected %q, DOM has %q", ErrOrderMismatch, p.Side, other.Side)
	}
	if p.Price != other.Price {
		return fmt.Errorf("%w: price expected %d, DOM has %d", ErrOrderMismatch, p.Price, other.Price)
	}
	if p.Quantity != other.Quantity {
		return fmt.Errorf("%w: quantity expected %d, DOM has %d", ErrOrderMismatch, p.Quantity, other.Quantity)
	}
	// The broker's estimated cost is only compared when it is present in the
	// UI; a missing value is zero and is not evidence of a mismatch, but a
	// present value that disagrees with price*quantity is.
	if other.EstimatedCost != 0 && other.EstimatedCost != other.ExpectedCost() {
		return fmt.Errorf("%w: DOM estimated cost %d does not equal price*quantity %d",
			ErrOrderMismatch, other.EstimatedCost, other.ExpectedCost())
	}
	return nil
}

// OrderStatus is the outcome reported by the broker after submission.
type OrderStatus string

const (
	OrderAccepted OrderStatus = "ACCEPTED"
	OrderRejected OrderStatus = "REJECTED"
	// OrderStatusUnknown means the broker response could not be interpreted.
	// It is never reported as success.
	OrderStatusUnknown OrderStatus = "UNKNOWN"
)

// OrderResult is the broker's response to a submitted order.
type OrderResult struct {
	Status OrderStatus
	// BrokerOrderID is the broker's reference when it exposes one.
	BrokerOrderID string
	// Message is the raw message shown by the broker UI.
	Message     string
	SubmittedAt time.Time
	Order       PreparedOrder
}
