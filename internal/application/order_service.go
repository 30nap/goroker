package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/logging"
)

// OrderOptions configures a single BUY run.
type OrderOptions struct {
	// WatchInterval is how often the price is re-read while waiting.
	WatchInterval time.Duration
	// MaxQuoteAge is the oldest a quote may be and still support a decision.
	MaxQuoteAge time.Duration
	// ConfirmationTimeout is how long the confirmation prompt stays valid.
	ConfirmationTimeout time.Duration
	// DryRun stops the flow after validation. A dry run can never submit.
	DryRun bool
	// SubmissionEnabled is the build-level switch for real submission.
	//
	// It is deliberately not a CLI flag and not a configuration key: it exists
	// so that the phases of this project can be brought up one at a time, and
	// it can only ever make Goroker safer. Submission additionally requires an
	// explicit user confirmation and a passing final validation; this switch
	// never substitutes for either.
	SubmissionEnabled bool
}

// OrderOutcome is the result of a BUY run.
type OrderOutcome struct {
	// Prepared is the order as read back from the broker's DOM.
	Prepared domain.PreparedOrder
	// Quote is the observation the decision was based on.
	Quote domain.Quote
	// Submitted is true only when an order was actually sent.
	Submitted bool
	// Result is the broker's response, when an order was submitted.
	Result domain.OrderResult
	// States is the path the state machine took.
	States []domain.State
}

// OrderService runs the BUY lifecycle.
//
// The safety property this type exists to uphold: SubmitPreparedOrder is
// reachable only after the user typed the confirmation word and a final
// re-validation of market, symbol, side, price, quantity and quote freshness
// succeeded. It is enforced three times over — by the state machine, by the
// authority object below, and by broker.Guard at the adapter boundary.
type OrderService struct {
	adapter   broker.Adapter
	watch     *WatchService
	confirmer Confirmer
	opts      OrderOptions
	log       *slog.Logger
	clock     Clock
}

// NewOrderService builds the service.
func NewOrderService(adapter broker.Adapter, confirmer Confirmer, opts OrderOptions, log *slog.Logger, clock Clock) *OrderService {
	if log == nil {
		log = slog.Default()
	}
	clock = orNow(clock)
	watch := NewWatchService(adapter, opts.WatchInterval, opts.MaxQuoteAge, log, clock)
	return &OrderService{
		adapter:   adapter,
		watch:     watch,
		confirmer: confirmer,
		opts:      opts,
		log:       log,
		clock:     clock,
	}
}

// submitAuthority records the facts that permit a submission. Every field must
// be true at the instant of the call, and the whole object is reset whenever
// anything about the order changes.
type submitAuthority struct {
	mu sync.Mutex

	machine        *domain.Machine
	enabled        bool
	dryRun         bool
	confirmed      bool
	finalValidated bool
	order          domain.PreparedOrder
}

func (a *submitAuthority) confirm() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmed = true
}

func (a *submitAuthority) finalValidate(order domain.PreparedOrder) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.finalValidated = true
	a.order = order
}

// revoke drops the authority. It is called whenever the flow returns to
// watching, so a later submission cannot ride on an earlier confirmation.
func (a *submitAuthority) revoke() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.confirmed = false
	a.finalValidated = false
	a.order = domain.PreparedOrder{}
}

// AuthorizeSubmit implements broker.SubmitAuthorizer.
func (a *submitAuthority) AuthorizeSubmit() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.dryRun {
		return fmt.Errorf("%w", domain.ErrDryRun)
	}
	if !a.enabled {
		return fmt.Errorf("%w: order submission is not enabled in this build", domain.ErrNotConfirmed)
	}
	if !a.confirmed {
		return fmt.Errorf("%w: no explicit confirmation was given", domain.ErrNotConfirmed)
	}
	if !a.finalValidated {
		return fmt.Errorf("%w: final validation did not run", domain.ErrNotConfirmed)
	}
	if a.machine == nil || !a.machine.MaySubmit() {
		return fmt.Errorf("%w: state machine is not in %s", domain.ErrInvalidTransition, domain.StateFinalValidation)
	}
	return nil
}

// Buy runs the full BUY lifecycle for req.
//
// The flow is: check authentication, check market, resolve the symbol exactly,
// read a live quote, watch until the target price window is reached, prepare
// the order, read it back from the DOM, validate it, ask for confirmation,
// validate everything again, and only then submit.
func (s *OrderService) Buy(ctx context.Context, req domain.BuyOrderRequest, onTick func(Observation)) (OrderOutcome, error) {
	if err := req.Validate(); err != nil {
		return OrderOutcome{}, err
	}

	machine := domain.NewMachine()
	authority := &submitAuthority{
		machine: machine,
		enabled: s.opts.SubmissionEnabled,
		dryRun:  s.opts.DryRun,
	}
	guarded := broker.NewGuard(s.adapter, authority)

	outcome := OrderOutcome{}
	fail := func(err error) (OrderOutcome, error) {
		machine.Abort()
		authority.revoke()
		outcome.States = machine.History()
		s.log.Error(logging.EventAborted, slog.String("error", err.Error()), slog.String("state", string(machine.Current())))
		return outcome, err
	}

	// --- authentication ---------------------------------------------------
	if err := RequireAuthenticated(ctx, s.adapter); err != nil {
		return fail(err)
	}
	if err := s.transition(machine, domain.StateAuthenticated); err != nil {
		return fail(err)
	}

	// --- market -----------------------------------------------------------
	market, err := s.adapter.MarketStatus(ctx)
	if err != nil {
		return fail(err)
	}
	if err := RequireOpenMarket(market); err != nil {
		return fail(err)
	}
	if err := s.transition(machine, domain.StateMarketOpen); err != nil {
		return fail(err)
	}

	// --- symbol -----------------------------------------------------------
	quotes := NewQuoteService(s.adapter, s.opts.MaxQuoteAge, s.log, s.clock)
	sym, err := quotes.Resolve(ctx, req.Symbol)
	if err != nil {
		return fail(err)
	}
	if err := s.transition(machine, domain.StateSymbolResolved); err != nil {
		return fail(err)
	}

	// --- watch ------------------------------------------------------------
	target, source := targetFor(req)
	if err := s.transition(machine, domain.StateWatching); err != nil {
		return fail(err)
	}
	obs, err := s.watch.Wait(ctx, sym, target, onTick)
	if err != nil {
		return fail(err)
	}
	if err := s.transition(machine, domain.StateTargetReached); err != nil {
		return fail(err)
	}
	outcome.Quote = obs.Quote

	// --- price selection --------------------------------------------------
	price, err := choosePrice(req, obs, source)
	if err != nil {
		return fail(err)
	}

	// --- prepare ----------------------------------------------------------
	if err := s.transition(machine, domain.StateOrderPreparing); err != nil {
		return fail(err)
	}
	expected, err := guarded.PrepareBuyOrder(ctx, req, price, source)
	if err != nil {
		return fail(err)
	}
	if err := s.transition(machine, domain.StateOrderPrepared); err != nil {
		return fail(err)
	}

	// --- read back and validate -------------------------------------------
	fromDOM, err := guarded.ReadPreparedOrder(ctx)
	if err != nil {
		return fail(err)
	}
	if err := s.validatePrepared(req, expected, fromDOM); err != nil {
		return fail(err)
	}
	fromDOM.PriceSource = source
	outcome.Prepared = fromDOM
	s.log.Info(logging.EventOrderValidated,
		slog.String("symbol", fromDOM.Symbol),
		slog.Int64("price", fromDOM.Price),
		slog.Int64("quantity", fromDOM.Quantity))
	if err := s.transition(machine, domain.StateOrderValidated); err != nil {
		return fail(err)
	}

	// --- dry run stops here ----------------------------------------------
	if s.opts.DryRun {
		s.log.Info(logging.EventDryRun, slog.String("reason", "dry-run never submits"))
		machine.Abort()
		outcome.States = machine.History()
		return outcome, nil
	}

	// --- confirmation -----------------------------------------------------
	if err := s.transition(machine, domain.StateAwaitingConfirm); err != nil {
		return fail(err)
	}
	s.log.Info(logging.EventAwaitingConfirmation, slog.Duration("timeout", s.opts.ConfirmationTimeout))
	prompt := ConfirmationPrompt{
		Order:          fromDOM,
		CurrentBestAsk: obs.Quote.BestAsk,
		MinPrice:       target.MinPrice,
		MaxPrice:       target.MaxPrice,
		QuoteAge:       obs.Quote.Age(s.clock()),
		Timeout:        s.opts.ConfirmationTimeout,
	}
	if s.confirmer == nil {
		return fail(fmt.Errorf("%w: no confirmation prompt is available", domain.ErrNotConfirmed))
	}
	if err := s.confirmer.Confirm(ctx, prompt); err != nil {
		switch {
		case errors.Is(err, domain.ErrConfirmationExpire):
			s.log.Warn(logging.EventConfirmationExpired)
		case errors.Is(err, domain.ErrNotConfirmed):
			s.log.Warn(logging.EventConfirmationRejected)
		}
		return fail(err)
	}
	authority.confirm()
	s.log.Info(logging.EventConfirmationReceived)
	if err := s.transition(machine, domain.StateConfirmed); err != nil {
		return fail(err)
	}

	// --- final validation -------------------------------------------------
	if err := s.transition(machine, domain.StateFinalValidation); err != nil {
		return fail(err)
	}
	finalQuote, err := s.finalValidation(ctx, guarded, req, sym, fromDOM, target, source)
	if err != nil {
		s.log.Error(logging.EventFinalValidationFailed, slog.String("error", err.Error()))
		return fail(err)
	}
	outcome.Quote = finalQuote
	authority.finalValidate(fromDOM)
	s.log.Info(logging.EventFinalValidationOK)

	// --- submit -----------------------------------------------------------
	result, err := guarded.SubmitPreparedOrder(ctx)
	if err != nil {
		return fail(err)
	}
	if err := s.transition(machine, domain.StateSubmitting); err != nil {
		return fail(err)
	}
	outcome.Submitted = true
	outcome.Result = result
	if result.Status != domain.OrderAccepted {
		return fail(fmt.Errorf("%w: broker status %s: %s", domain.ErrUnknownOrderState, result.Status, result.Message))
	}
	if err := s.transition(machine, domain.StateAccepted); err != nil {
		return fail(err)
	}
	outcome.States = machine.History()
	return outcome, nil
}

// validatePrepared compares the order read back from the DOM against both the
// order Goroker intended and the user's original request. Any difference
// aborts; nothing is silently corrected.
func (s *OrderService) validatePrepared(req domain.BuyOrderRequest, expected, fromDOM domain.PreparedOrder) error {
	if err := expected.Matches(fromDOM); err != nil {
		return err
	}
	if fromDOM.Side != domain.SideBuy {
		return fmt.Errorf("%w: order form shows side %q", domain.ErrOrderMismatch, fromDOM.Side)
	}
	if !broker.SymbolsEqual(fromDOM.Symbol, req.Symbol) {
		return fmt.Errorf("%w: order form shows symbol %q, requested %q",
			domain.ErrOrderMismatch, fromDOM.Symbol, req.Symbol)
	}
	if fromDOM.Quantity != req.Quantity {
		return fmt.Errorf("%w: order form shows quantity %d, requested %d",
			domain.ErrOrderMismatch, fromDOM.Quantity, req.Quantity)
	}
	if !req.InRange(fromDOM.Price) {
		min, max := req.Bounds()
		return fmt.Errorf("%w: order price %d is outside %d..%d",
			domain.ErrPriceOutOfRange, fromDOM.Price, min, max)
	}
	return nil
}

// finalValidation re-reads everything that could have changed between showing
// the confirmation prompt and clicking submit.
func (s *OrderService) finalValidation(
	ctx context.Context,
	adapter broker.Adapter,
	req domain.BuyOrderRequest,
	sym domain.Symbol,
	prepared domain.PreparedOrder,
	target Target,
	source domain.PriceSource,
) (domain.Quote, error) {
	if err := ctx.Err(); err != nil {
		return domain.Quote{}, fmt.Errorf("%w: %v", domain.ErrCancelled, err)
	}

	market, err := adapter.MarketStatus(ctx)
	if err != nil {
		return domain.Quote{}, err
	}
	if err := RequireOpenMarket(market); err != nil {
		return domain.Quote{}, err
	}

	quotes := NewQuoteService(s.adapter, s.opts.MaxQuoteAge, s.log, s.clock)
	q, err := quotes.Quote(ctx, sym)
	if err != nil {
		return domain.Quote{}, err
	}

	// The order form must still contain exactly what was confirmed.
	current, err := adapter.ReadPreparedOrder(ctx)
	if err != nil {
		return domain.Quote{}, err
	}
	confirmed := prepared
	confirmed.EstimatedCost = 0
	if err := confirmed.Matches(current); err != nil {
		return domain.Quote{}, err
	}
	if err := s.validatePrepared(req, confirmed, current); err != nil {
		return domain.Quote{}, err
	}

	// In range mode the market must still be offering a price inside the
	// window; a price that has run away since the prompt aborts.
	if req.Mode() == domain.PriceModeRange {
		live, err := q.Price(source)
		if err != nil {
			return domain.Quote{}, err
		}
		if !target.Contains(live) {
			return domain.Quote{}, fmt.Errorf("%w: %s moved to %d, outside %d..%d",
				domain.ErrPriceOutOfRange, source, live, target.MinPrice, target.MaxPrice)
		}
	}
	return q, nil
}

func (s *OrderService) transition(m *domain.Machine, next domain.State) error {
	if err := m.To(next); err != nil {
		return err
	}
	s.log.Debug(logging.EventStateChange, slog.String("state", string(next)))
	return nil
}

// targetFor derives the watch target and price source from the request.
//
// Range mode waits for the best ask to enter [min, max] and uses the best ask
// as the order price. Exact mode waits for the best ask to reach exactly the
// requested price and then uses that requested price, unchanged.
func targetFor(req domain.BuyOrderRequest) (Target, domain.PriceSource) {
	min, max := req.Bounds()
	if req.Mode() == domain.PriceModeExact {
		return Target{MinPrice: min, MaxPrice: max, Source: domain.PriceSourceBestAsk}, domain.PriceSourceExplicit
	}
	return Target{MinPrice: min, MaxPrice: max, Source: domain.PriceSourceBestAsk}, domain.PriceSourceBestAsk
}

// choosePrice picks the order price.
//
// In range mode it is the observed best ask, which the watch has already shown
// to be inside the window. In exact mode it is the price the user asked for,
// never altered. Either way the chosen price is re-checked against the
// requested bounds before it is used.
func choosePrice(req domain.BuyOrderRequest, obs Observation, source domain.PriceSource) (int64, error) {
	var price int64
	switch source {
	case domain.PriceSourceExplicit:
		price = req.Price
	case domain.PriceSourceBestAsk:
		price = obs.Quote.BestAsk
	default:
		return 0, fmt.Errorf("%w: unknown price source %q", domain.ErrAbort, source)
	}
	if price <= 0 {
		return 0, fmt.Errorf("%w: chosen price is %d", domain.ErrQuoteInvalid, price)
	}
	if !req.InRange(price) {
		min, max := req.Bounds()
		return 0, fmt.Errorf("%w: chosen price %d is outside %d..%d", domain.ErrPriceOutOfRange, price, min, max)
	}
	return price, nil
}
