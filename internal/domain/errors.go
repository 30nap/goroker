package domain

import "errors"

// Goroker fails closed: every uncertain condition maps onto one of these
// sentinel errors, and every one of them aborts the current operation.
var (
	// ErrAbort is the generic fail-closed error. Callers may wrap it with
	// context; commands treat any error wrapping ErrAbort as ABORT.
	ErrAbort = errors.New("aborted")

	ErrNotAuthenticated   = errors.New("not authenticated")
	ErrAuthUncertain      = errors.New("authentication state uncertain")
	ErrMarketNotOpen      = errors.New("market is not open")
	ErrMarketUnknown      = errors.New("market status unknown")
	ErrSymbolNotFound     = errors.New("symbol not found")
	ErrSymbolAmbiguous    = errors.New("symbol is ambiguous")
	ErrQuoteStale         = errors.New("quote is stale")
	ErrQuoteInvalid       = errors.New("quote is invalid")
	ErrPriceOutOfRange    = errors.New("price outside requested range")
	ErrOrderMismatch      = errors.New("prepared order does not match expected order")
	ErrSelectorMissing    = errors.New("required UI selector not found")
	ErrUnexpectedModal    = errors.New("unexpected modal or popup")
	ErrBrowserUnavailable = errors.New("browser unavailable")
	ErrDOMChanged         = errors.New("broker DOM changed")
	ErrUnknownOrderState  = errors.New("unknown order state")
	ErrNotConfirmed       = errors.New("order not confirmed by user")
	ErrConfirmationExpire = errors.New("confirmation timed out")
	ErrInvalidTransition  = errors.New("invalid state transition")
	ErrDryRun             = errors.New("dry-run: submission is not permitted")
	ErrNotImplemented     = errors.New("not implemented for this broker yet")
	ErrCancelled          = errors.New("cancelled")
)
