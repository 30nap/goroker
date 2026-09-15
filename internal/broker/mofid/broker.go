package mofid

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/go-rod/rod"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/logging"
	"github.com/30nap/goroker/internal/storage"
)

// AdapterName is the value of broker.name in config.yaml that selects this
// adapter.
const AdapterName = "mofid"

func init() {
	broker.Register(AdapterName, func(deps broker.Deps) (broker.Adapter, error) {
		return New(deps)
	})
}

// Broker drives the Mofid Easy Trader web UI through Rod.
//
// It holds no brokerage knowledge beyond the URLs in the configuration and the
// selectors in selectors.go, and it never submits an order on its own: the
// order service calls SubmitPreparedOrder only after explicit confirmation and
// a passing final validation, and broker.Guard blocks the call otherwise.
type Broker struct {
	deps broker.Deps
	sel  Selectors
	log  *slog.Logger

	mu   sync.Mutex
	page *rod.Page
	// prepared is what Goroker last typed into the order form. It is used
	// only for logging and for the mismatch message; validation always
	// compares against what is read back from the DOM.
	prepared *domain.PreparedOrder
}

// New builds the adapter. It loads selector overrides from
// ~/.goroker/selectors.yaml when that file exists.
func New(deps broker.Deps) (*Broker, error) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	sel, err := Load(filepath.Join(deps.Config.Dir, SelectorsFileName))
	if err != nil {
		return nil, err
	}
	return &Broker{deps: deps, sel: sel, log: deps.Logger}, nil
}

// Name implements broker.Adapter.
func (b *Broker) Name() string { return AdapterName }

// Selectors exposes the loaded selector set so `goroker status` can report
// which parts of broker discovery are still outstanding.
func (b *Broker) Selectors() Selectors { return b.sel }

// MissingSelectors lists the selectors that broker discovery has not filled in
// yet. While this is non-empty, trading operations fail closed.
func (b *Broker) MissingSelectors() []string { return b.sel.Missing() }

// Close releases the adapter's page. The browser itself is owned by the caller.
func (b *Broker) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.page != nil {
		_ = b.page.Close()
		b.page = nil
	}
	return nil
}

// ---------------------------------------------------------------------------
// page management
// ---------------------------------------------------------------------------

// tradingPage returns the trading panel page, opening it once and then reusing
// it. Reuse matters: the broker UI updates prices over its own live
// connection, and reloading the page would throw those updates away.
func (b *Broker) tradingPage(ctx context.Context) (*pageCtx, error) {
	url := b.deps.Config.Broker.TradingURL
	if url == "" {
		url = b.deps.Config.Broker.BaseURL
	}
	return b.pageFor(ctx, url)
}

func (b *Broker) pageFor(ctx context.Context, url string) (*pageCtx, error) {
	if url == "" {
		return nil, fmt.Errorf("%w: no broker URL configured; set broker.base_url in config.yaml", domain.ErrAbort)
	}
	if b.deps.Browser == nil {
		return nil, domain.ErrBrowserUnavailable
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if b.page == nil {
		page, err := b.deps.Browser.Page(ctx, url)
		if err != nil {
			return nil, err
		}
		b.page = page.CancelTimeout()
		if err := b.page.Context(ctx).WaitLoad(); err != nil {
			return nil, fmt.Errorf("%w: load %s: %v", domain.ErrBrowserUnavailable, url, err)
		}
	}
	return newPageCtx(b.page, b.sel, b.log, b.deps.Browser.Timeout()), nil
}

// navigate points the existing page at url, which keeps the session and avoids
// opening more tabs than necessary.
func (b *Broker) navigate(ctx context.Context, p *pageCtx, url string) error {
	if err := p.page.Context(ctx).Navigate(url); err != nil {
		return fmt.Errorf("%w: navigate to %s: %v", domain.ErrBrowserUnavailable, url, err)
	}
	if err := p.page.Context(ctx).WaitLoad(); err != nil {
		return fmt.Errorf("%w: load %s: %v", domain.ErrBrowserUnavailable, url, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// authentication
// ---------------------------------------------------------------------------

// IsAuthenticated implements broker.Adapter.
//
// The answer is three-valued in practice: authenticated, not authenticated, or
// uncertain. Uncertainty is returned as domain.ErrAuthUncertain so that callers
// abort instead of assuming a session exists.
func (b *Broker) IsAuthenticated(ctx context.Context) (bool, error) {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return false, err
	}
	return b.authState(ctx, p)
}

func (b *Broker) authState(ctx context.Context, p *pageCtx) (bool, error) {
	marker, err := Require("logged_in_marker", b.sel.LoggedInMarker)
	if err != nil {
		return false, err
	}
	loggedIn, err := p.present(ctx, marker, 3*time.Second)
	if err != nil {
		return false, err
	}
	if loggedIn {
		return true, nil
	}

	// Not seeing the marker is not proof of being logged out: the page may
	// still be rendering. Only a visible login form is positive evidence.
	if b.sel.LoginForm != "" {
		loginVisible, err := p.present(ctx, b.sel.LoginForm, 2*time.Second)
		if err != nil {
			return false, err
		}
		if loginVisible {
			return false, nil
		}
	}
	if challenge, ok, err := p.challenge(ctx); err != nil {
		return false, err
	} else if ok {
		return false, fmt.Errorf("%w: a security challenge is on screen (%s); run `goroker login` and complete it manually",
			domain.ErrAuthUncertain, challenge)
	}
	return false, fmt.Errorf("%w: neither the authenticated marker nor the login form is present", domain.ErrAuthUncertain)
}

// Login implements broker.Adapter.
//
// Credentials are filled only when they are configured locally. Any security
// challenge pauses automation: Goroker waits for the user to complete it in the
// visible browser window and never attempts to solve or bypass one.
func (b *Broker) Login(ctx context.Context) error {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return err
	}

	if ok, err := b.authState(ctx, p); err == nil && ok {
		b.log.Info(logging.EventSessionRestored)
		return nil
	}

	loginURL := b.deps.Config.Broker.LoginURL
	if loginURL != "" {
		if err := b.navigate(ctx, p, loginURL); err != nil {
			return err
		}
	}
	b.log.Info(logging.EventLoginRequired, slog.String("url", loginURL))

	creds, source, err := b.deps.Credentials.Load(b.credentialAccount())
	switch {
	case err == nil:
		// Values are used here and nowhere else; they are never logged.
		b.log.Info("LOGIN_CREDENTIALS_AVAILABLE", slog.String("source", string(source)))
		if err := b.fillLoginForm(ctx, p, creds); err != nil {
			return err
		}
	case errors.Is(err, storage.ErrNoCredentials):
		b.log.Info("LOGIN_MANUAL", slog.String("reason", "no stored credentials"))
	default:
		return err
	}

	return b.waitForAuthentication(ctx, p)
}

func (b *Broker) fillLoginForm(ctx context.Context, p *pageCtx, creds storage.Credentials) error {
	if err := p.fill(ctx, "username_input", b.sel.UsernameInput, creds.Username); err != nil {
		return err
	}
	if err := p.fill(ctx, "password_input", b.sel.PasswordInput, creds.Password); err != nil {
		return err
	}

	// A challenge may already be on screen next to the form. If so, submitting
	// is the user's job, not ours.
	if name, ok, err := p.challenge(ctx); err != nil {
		return err
	} else if ok {
		b.log.Info(logging.EventLoginChallenge, slog.String("marker", name))
		return nil
	}
	return p.click(ctx, "login_button", b.sel.LoginButton)
}

// waitForAuthentication polls until the site reports an authenticated session,
// giving the user time to complete any challenge by hand.
func (b *Broker) waitForAuthentication(ctx context.Context, p *pageCtx) error {
	deadline := time.Now().Add(b.deps.Config.Browser.ManualChallengeTimeout)
	announced := false

	for {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %v", domain.ErrCancelled, err)
		}
		ok, err := b.authState(ctx, p)
		if err == nil && ok {
			b.log.Info(logging.EventLoginSuccess)
			return nil
		}
		if name, found, cerr := p.challenge(ctx); cerr == nil && found && !announced {
			announced = true
			b.log.Warn(logging.EventLoginChallenge, slog.String("marker", name))
			fmt.Println("A security challenge is shown in the browser window.")
			fmt.Println("Please complete it there. Goroker will continue once the site reports you are signed in.")
		}
		if b.sel.LoginError != "" {
			if msg, terr := p.text(ctx, "login_error", b.sel.LoginError); terr == nil && msg != "" {
				b.log.Error(logging.EventLoginFailed, slog.String("message", msg))
				return fmt.Errorf("%w: broker reported: %s", domain.ErrNotAuthenticated, msg)
			}
		}
		if time.Now().After(deadline) {
			b.log.Error(logging.EventLoginFailed, slog.String("reason", "timeout"))
			return fmt.Errorf("%w: gave up waiting for a signed-in session", domain.ErrNotAuthenticated)
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v", domain.ErrCancelled, ctx.Err())
		case <-time.After(2 * time.Second):
		}
	}
}

// credentialAccount is the keyring account label. It is derived from the broker
// name, never from the brokerage username.
func (b *Broker) credentialAccount() string {
	if name := b.deps.Config.Broker.Name; name != "" {
		return name
	}
	return AdapterName
}

// ---------------------------------------------------------------------------
// market status
// ---------------------------------------------------------------------------

// MarketStatus implements broker.Adapter. Any text that is not positively an
// open or closed label yields MarketUnknown, which is never treated as open.
func (b *Broker) MarketStatus(ctx context.Context) (domain.MarketStatus, error) {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return domain.UnknownMarket("", time.Now()), err
	}
	raw, err := p.text(ctx, "market_status_indicator", b.sel.MarketStatusIndicator)
	if err != nil {
		return domain.UnknownMarket("", time.Now()), err
	}

	status := domain.MarketStatus{Raw: raw, ObservedAt: time.Now(), State: domain.MarketUnknown}
	switch {
	case matchAny(raw, b.sel.MarketOpenText):
		status.State = domain.MarketOpen
		b.log.Info(logging.EventMarketOpen, slog.String("raw", raw))
	case matchAny(raw, b.sel.MarketClosedText):
		status.State = domain.MarketClosed
		b.log.Info(logging.EventMarketClosed, slog.String("raw", raw))
	default:
		b.log.Warn(logging.EventMarketUnknown, slog.String("raw", raw))
	}
	return status, nil
}

// ---------------------------------------------------------------------------
// symbol and quote
// ---------------------------------------------------------------------------

// ResolveSymbol implements broker.Adapter. Matching is exact: several matches
// abort rather than pick one.
func (b *Broker) ResolveSymbol(ctx context.Context, symbol string) (domain.Symbol, error) {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return domain.Symbol{}, err
	}
	if err := p.assertNoUnexpectedModal(ctx); err != nil {
		return domain.Symbol{}, err
	}
	if err := p.fill(ctx, "symbol_search", b.sel.SymbolSearch, symbol); err != nil {
		return domain.Symbol{}, err
	}

	resultSel, err := Require("symbol_result", b.sel.SymbolResult)
	if err != nil {
		return domain.Symbol{}, err
	}
	// Give the result list a moment to populate before reading it.
	if _, err := p.element(ctx, "symbol_result", resultSel); err != nil {
		return domain.Symbol{}, fmt.Errorf("%w: no result for symbol %q", domain.ErrSymbolNotFound, symbol)
	}
	elements, err := p.page.Context(ctx).Elements(resultSel)
	if err != nil {
		return domain.Symbol{}, fmt.Errorf("%w: read symbol results: %v", domain.ErrDOMChanged, err)
	}

	var matches []domain.Symbol
	for _, el := range elements {
		name, err := elementText(el, b.sel.SymbolResultName)
		if err != nil {
			return domain.Symbol{}, err
		}
		if !broker.SymbolsEqual(name, symbol) {
			continue
		}
		title, _ := elementText(el, b.sel.SymbolResultTitle)
		id, _ := elementAttr(el, "data-instrument-id")
		matches = append(matches, domain.Symbol{Name: name, Title: title, BrokerID: id})
	}

	switch len(matches) {
	case 0:
		return domain.Symbol{}, fmt.Errorf("%w: %q", domain.ErrSymbolNotFound, symbol)
	case 1:
		b.log.Info(logging.EventSymbolResolved, slog.String("symbol", matches[0].Name))
		return matches[0], nil
	default:
		return domain.Symbol{}, fmt.Errorf("%w: %q matched %d instruments; be more specific",
			domain.ErrSymbolAmbiguous, symbol, len(matches))
	}
}

// Quote implements broker.Adapter. The observation timestamp is taken at the
// moment the values are read, and freshness is judged by the caller.
func (b *Broker) Quote(ctx context.Context, symbol domain.Symbol) (domain.Quote, error) {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return domain.Quote{}, err
	}
	if err := p.assertNoUnexpectedModal(ctx); err != nil {
		return domain.Quote{}, err
	}

	// The instrument shown must be the one that was resolved, otherwise the
	// prices on screen belong to something else.
	if b.sel.SelectedSymbol != "" {
		shown, err := p.text(ctx, "selected_symbol", b.sel.SelectedSymbol)
		if err != nil {
			return domain.Quote{}, err
		}
		if !broker.SymbolsEqual(shown, symbol.Name) {
			return domain.Quote{}, fmt.Errorf("%w: page shows %q while %q was requested",
				domain.ErrOrderMismatch, shown, symbol.Name)
		}
	}

	observedAt := time.Now()
	last, err := p.number(ctx, "last_price", b.sel.LastPrice)
	if err != nil {
		return domain.Quote{}, err
	}
	ask, err := p.number(ctx, "best_ask", b.sel.BestAsk)
	if err != nil {
		return domain.Quote{}, err
	}
	bid, err := p.number(ctx, "best_bid", b.sel.BestBid)
	if err != nil {
		return domain.Quote{}, err
	}

	q := domain.Quote{
		Symbol:    symbol.Name,
		LastPrice: last,
		BestAsk:   ask,
		BestBid:   bid,
		Timestamp: observedAt,
	}
	if err := q.Validate(); err != nil {
		return domain.Quote{}, err
	}
	b.log.Debug(logging.EventQuoteRead,
		slog.String("symbol", q.Symbol),
		slog.Int64("last", q.LastPrice),
		slog.Int64("best_ask", q.BestAsk),
		slog.Int64("best_bid", q.BestBid))
	return q, nil
}

// ---------------------------------------------------------------------------
// order form
// ---------------------------------------------------------------------------

// PrepareBuyOrder fills the BUY order form and does not submit it.
func (b *Broker) PrepareBuyOrder(ctx context.Context, req domain.BuyOrderRequest, price int64, source domain.PriceSource) (domain.PreparedOrder, error) {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return domain.PreparedOrder{}, err
	}
	if err := p.assertNoUnexpectedModal(ctx); err != nil {
		return domain.PreparedOrder{}, err
	}

	b.log.Info(logging.EventOrderPreparing,
		slog.String("symbol", req.Symbol),
		slog.Int64("price", price),
		slog.Int64("quantity", req.Quantity),
		slog.String("price_source", string(source)))

	if err := p.click(ctx, "buy_tab", b.sel.BuyTab); err != nil {
		return domain.PreparedOrder{}, err
	}
	if err := b.checkPriceLimits(ctx, p, price); err != nil {
		return domain.PreparedOrder{}, err
	}
	if err := p.fill(ctx, "price_input", b.sel.PriceInput, strconv.FormatInt(price, 10)); err != nil {
		return domain.PreparedOrder{}, err
	}
	if err := p.fill(ctx, "quantity_input", b.sel.QuantityInput, strconv.FormatInt(req.Quantity, 10)); err != nil {
		return domain.PreparedOrder{}, err
	}

	prepared := domain.PreparedOrder{
		Symbol:        req.Symbol,
		Side:          domain.SideBuy,
		Price:         price,
		Quantity:      req.Quantity,
		EstimatedCost: price * req.Quantity,
		PriceSource:   source,
		PreparedAt:    time.Now(),
	}
	b.mu.Lock()
	b.prepared = &prepared
	b.mu.Unlock()
	return prepared, nil
}

// checkPriceLimits verifies the requested price against the daily price band
// when the UI shows one. An unreadable band is not fatal, but a price outside a
// band that was read successfully is.
func (b *Broker) checkPriceLimits(ctx context.Context, p *pageCtx, price int64) error {
	minEl, ok, err := p.optionalElement(ctx, b.sel.PriceLimitMin, time.Second)
	if err != nil || !ok {
		return err
	}
	minText, err := minEl.Text()
	if err != nil {
		return nil
	}
	maxEl, ok, err := p.optionalElement(ctx, b.sel.PriceLimitMax, time.Second)
	if err != nil || !ok {
		return err
	}
	maxText, err := maxEl.Text()
	if err != nil {
		return nil
	}

	low, lerr := broker.ParsePrice(minText)
	high, herr := broker.ParsePrice(maxText)
	if lerr != nil || herr != nil {
		return nil
	}
	if price < low || price > high {
		return fmt.Errorf("%w: price %d is outside the broker's allowed band %d..%d",
			domain.ErrPriceOutOfRange, price, low, high)
	}
	return nil
}

// ReadPreparedOrder reads the order form back from the live DOM. It reads the
// form's own values rather than trusting what was typed.
func (b *Broker) ReadPreparedOrder(ctx context.Context) (domain.PreparedOrder, error) {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return domain.PreparedOrder{}, err
	}
	if err := p.assertNoUnexpectedModal(ctx); err != nil {
		return domain.PreparedOrder{}, err
	}

	symbol, err := p.text(ctx, "order_symbol_field", b.sel.OrderSymbolField)
	if err != nil {
		return domain.PreparedOrder{}, err
	}
	sideText, err := p.text(ctx, "order_side_mark", b.sel.OrderSideMark)
	if err != nil {
		return domain.PreparedOrder{}, err
	}
	priceText, err := p.inputValue(ctx, "price_input", b.sel.PriceInput)
	if err != nil {
		return domain.PreparedOrder{}, err
	}
	quantityText, err := p.inputValue(ctx, "quantity_input", b.sel.QuantityInput)
	if err != nil {
		return domain.PreparedOrder{}, err
	}

	price, err := broker.ParsePrice(priceText)
	if err != nil {
		return domain.PreparedOrder{}, fmt.Errorf("%w: price field: %v", domain.ErrDOMChanged, err)
	}
	quantity, err := broker.ParsePrice(quantityText)
	if err != nil {
		return domain.PreparedOrder{}, fmt.Errorf("%w: quantity field: %v", domain.ErrDOMChanged, err)
	}

	order := domain.PreparedOrder{
		Symbol:     symbol,
		Side:       b.readSide(sideText),
		Price:      price,
		Quantity:   quantity,
		PreparedAt: time.Now(),
	}
	if costEl, ok, err := p.optionalElement(ctx, b.sel.EstimatedCost, time.Second); err == nil && ok {
		if text, terr := costEl.Text(); terr == nil {
			if cost, cerr := broker.ParsePrice(text); cerr == nil {
				order.EstimatedCost = cost
			}
		}
	}
	b.log.Info(logging.EventOrderPrepared,
		slog.String("symbol", order.Symbol),
		slog.String("side", string(order.Side)),
		slog.Int64("price", order.Price),
		slog.Int64("quantity", order.Quantity))
	return order, nil
}

// readSide maps the order form's side label onto the domain type. An
// unrecognised label yields an empty side, which fails the comparison against
// the expected BUY order and therefore aborts.
func (b *Broker) readSide(text string) domain.OrderSide {
	switch {
	case matchAny(text, []string{"BUY", "buy", "خرید"}):
		return domain.SideBuy
	case matchAny(text, []string{"SELL", "sell", "فروش"}):
		return domain.SideSell
	default:
		return domain.OrderSide("")
	}
}

// SubmitPreparedOrder clicks submit and interprets the broker's response.
//
// This method must only ever be reached through the order service, after
// explicit user confirmation and a passing final validation; broker.Guard
// refuses the call otherwise.
func (b *Broker) SubmitPreparedOrder(ctx context.Context) (domain.OrderResult, error) {
	p, err := b.tradingPage(ctx)
	if err != nil {
		return domain.OrderResult{}, err
	}

	b.mu.Lock()
	prepared := b.prepared
	b.mu.Unlock()
	if prepared == nil {
		return domain.OrderResult{}, fmt.Errorf("%w: no order has been prepared", domain.ErrUnknownOrderState)
	}

	b.log.Info(logging.EventOrderSubmitting,
		slog.String("symbol", prepared.Symbol),
		slog.Int64("price", prepared.Price),
		slog.Int64("quantity", prepared.Quantity))

	if err := p.click(ctx, "submit_button", b.sel.SubmitButton); err != nil {
		return domain.OrderResult{}, err
	}
	// Some broker UIs show their own confirmation dialog. Accepting it is part
	// of the submission the user already confirmed.
	if b.sel.SubmitConfirmDialog != "" {
		if ok, derr := p.present(ctx, b.sel.SubmitConfirmDialog, 3*time.Second); derr == nil && ok {
			if err := p.click(ctx, "submit_confirm_accept", b.sel.SubmitConfirmAccept); err != nil {
				return domain.OrderResult{}, err
			}
		}
	}

	result := domain.OrderResult{
		Status:      domain.OrderStatusUnknown,
		SubmittedAt: time.Now(),
		Order:       *prepared,
	}
	message, err := p.text(ctx, "result_message", b.sel.ResultMessage)
	if err != nil {
		return result, fmt.Errorf("%w: could not read the broker's response; check the order in the broker UI: %v",
			domain.ErrUnknownOrderState, err)
	}
	result.Message = message

	switch {
	case containsAny(message, b.sel.ResultAcceptedText):
		result.Status = domain.OrderAccepted
		b.log.Info(logging.EventOrderAccepted, slog.String("message", message))
	case containsAny(message, b.sel.ResultRejectedText):
		result.Status = domain.OrderRejected
		b.log.Error(logging.EventOrderRejected, slog.String("message", message))
	default:
		b.log.Error(logging.EventOrderRejected, slog.String("message", message), slog.String("reason", "unrecognised response"))
		return result, fmt.Errorf("%w: broker said %q; check the order in the broker UI", domain.ErrUnknownOrderState, message)
	}

	if idEl, ok, err := p.optionalElement(ctx, b.sel.ResultOrderID, time.Second); err == nil && ok {
		if text, terr := idEl.Text(); terr == nil {
			result.BrokerOrderID = text
		}
	}
	return result, nil
}

func elementText(el *rod.Element, selector string) (string, error) {
	if selector == "" {
		text, err := el.Text()
		if err != nil {
			return "", fmt.Errorf("%w: read result text: %v", domain.ErrDOMChanged, err)
		}
		return text, nil
	}
	child, err := el.Element(selector)
	if err != nil {
		return "", fmt.Errorf("%w: %s inside search result: %v", domain.ErrSelectorMissing, selector, err)
	}
	text, err := child.Text()
	if err != nil {
		return "", fmt.Errorf("%w: read %s: %v", domain.ErrDOMChanged, selector, err)
	}
	return text, nil
}

func elementAttr(el *rod.Element, name string) (string, error) {
	value, err := el.Attribute(name)
	if err != nil || value == nil {
		return "", err
	}
	return *value, nil
}

// compile-time check that the adapter satisfies the interface.
var _ broker.Adapter = (*Broker)(nil)
