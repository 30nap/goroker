// Package logging sets up Goroker's structured logging and defines the
// vocabulary of events the application emits.
//
// Credentials are never logged. The handler in this package strips any
// attribute whose key looks like a secret, so a mistake elsewhere in the
// codebase cannot leak a password, OTP, cookie or token into the log.
package logging

// Event names used with slog. Every log line carries one of these as its
// message, which makes logs greppable and stable across refactors.
const (
	EventAppStart        = "APP_START"
	EventBrowserStarted  = "BROWSER_STARTED"
	EventBrowserClosed   = "BROWSER_CLOSED"
	EventSessionRestored = "SESSION_RESTORED"

	EventLoginRequired  = "LOGIN_REQUIRED"
	EventLoginChallenge = "LOGIN_CHALLENGE_MANUAL"
	EventLoginSuccess   = "LOGIN_SUCCESS"
	EventLoginFailed    = "LOGIN_FAILED"

	EventMarketOpen    = "MARKET_OPEN"
	EventMarketClosed  = "MARKET_CLOSED"
	EventMarketUnknown = "MARKET_UNKNOWN"

	EventSymbolResolved = "SYMBOL_RESOLVED"
	EventQuoteRead      = "QUOTE_READ"

	EventWatchingPrice     = "WATCHING_PRICE"
	EventTargetPriceReache = "TARGET_PRICE_REACHED"

	EventOrderPreparing = "ORDER_PREPARING"
	EventOrderPrepared  = "ORDER_PREPARED"
	EventOrderValidated = "ORDER_VALIDATED"

	EventAwaitingConfirmation = "AWAITING_CONFIRMATION"
	EventConfirmationReceived = "CONFIRMATION_RECEIVED"
	EventConfirmationRejected = "CONFIRMATION_REJECTED"
	EventConfirmationExpired  = "CONFIRMATION_EXPIRED"

	EventFinalValidationOK     = "FINAL_VALIDATION_OK"
	EventFinalValidationFailed = "FINAL_VALIDATION_FAILED"

	EventOrderSubmitting = "ORDER_SUBMITTING"
	EventOrderAccepted   = "ORDER_ACCEPTED"
	EventOrderRejected   = "ORDER_REJECTED"

	EventDryRun      = "DRY_RUN_STOP"
	EventScreenshot  = "DEBUG_SCREENSHOT_SAVED"
	EventStateChange = "STATE_CHANGE"
	EventAborted     = "ABORTED"
)
