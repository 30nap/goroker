package application_test

import (
	"context"
	"sync"
	"time"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/domain"
)

// fakeAdapter is a broker.Adapter that never touches a browser. Every test in
// this package drives the order service through it, so the safety rules are
// exercised without the real brokerage.
type fakeAdapter struct {
	mu sync.Mutex

	authenticated bool
	authErr       error

	market    domain.MarketStatus
	markets   []domain.MarketStatus // consumed in order; the last one repeats
	marketErr error

	symbol     domain.Symbol
	resolveErr error

	quotes     []domain.Quote // consumed in order; the last one repeats
	quoteErr   error
	quoteCalls int

	prepareErr error
	// domOrders is what ReadPreparedOrder returns, consumed in order; the last
	// one repeats. When empty, the form mirrors what was prepared.
	domOrders []domain.PreparedOrder
	readErr   error
	readCalls int

	submitErr   error
	submitCalls int
	result      domain.OrderResult

	lastPrepared domain.PreparedOrder
	// onQuote runs before every quote is served, which lets a test change the
	// world mid-flow (cancel the context, close the market, ...).
	onQuote func(call int)
}

var _ broker.Adapter = (*fakeAdapter)(nil)

func (f *fakeAdapter) Name() string { return "fake" }

func (f *fakeAdapter) Login(context.Context) error { return nil }

func (f *fakeAdapter) IsAuthenticated(context.Context) (bool, error) {
	if f.authErr != nil {
		return false, f.authErr
	}
	return f.authenticated, nil
}

func (f *fakeAdapter) MarketStatus(context.Context) (domain.MarketStatus, error) {
	if f.marketErr != nil {
		return domain.UnknownMarket("", time.Now()), f.marketErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.markets) == 0 {
		return f.market, nil
	}
	next := f.markets[0]
	if len(f.markets) > 1 {
		f.markets = f.markets[1:]
	}
	return next, nil
}

func (f *fakeAdapter) ResolveSymbol(_ context.Context, symbol string) (domain.Symbol, error) {
	if f.resolveErr != nil {
		return domain.Symbol{}, f.resolveErr
	}
	if f.symbol.Name != "" {
		return f.symbol, nil
	}
	return domain.Symbol{Name: symbol}, nil
}

func (f *fakeAdapter) Quote(_ context.Context, sym domain.Symbol) (domain.Quote, error) {
	f.mu.Lock()
	call := f.quoteCalls
	f.quoteCalls++
	hook := f.onQuote
	f.mu.Unlock()

	if hook != nil {
		hook(call)
	}
	if f.quoteErr != nil {
		return domain.Quote{}, f.quoteErr
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.quotes) == 0 {
		return domain.Quote{}, domain.ErrQuoteInvalid
	}
	q := f.quotes[0]
	if len(f.quotes) > 1 {
		f.quotes = f.quotes[1:]
	}
	if q.Symbol == "" {
		q.Symbol = sym.Name
	}
	if q.Timestamp.IsZero() {
		q.Timestamp = time.Now()
	}
	return q, nil
}

func (f *fakeAdapter) PrepareBuyOrder(_ context.Context, req domain.BuyOrderRequest, price int64, source domain.PriceSource) (domain.PreparedOrder, error) {
	if f.prepareErr != nil {
		return domain.PreparedOrder{}, f.prepareErr
	}
	order := domain.PreparedOrder{
		Symbol:      req.Symbol,
		Side:        domain.SideBuy,
		Price:       price,
		Quantity:    req.Quantity,
		PriceSource: source,
		PreparedAt:  time.Now(),
	}
	f.mu.Lock()
	f.lastPrepared = order
	f.mu.Unlock()
	return order, nil
}

func (f *fakeAdapter) ReadPreparedOrder(context.Context) (domain.PreparedOrder, error) {
	if f.readErr != nil {
		return domain.PreparedOrder{}, f.readErr
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.readCalls++
	if len(f.domOrders) == 0 {
		return f.lastPrepared, nil
	}
	next := f.domOrders[0]
	if len(f.domOrders) > 1 {
		f.domOrders = f.domOrders[1:]
	}
	return next, nil
}

func (f *fakeAdapter) SubmitPreparedOrder(context.Context) (domain.OrderResult, error) {
	f.mu.Lock()
	f.submitCalls++
	f.mu.Unlock()
	if f.submitErr != nil {
		return domain.OrderResult{}, f.submitErr
	}
	if f.result.Status == "" {
		f.result = domain.OrderResult{Status: domain.OrderAccepted, BrokerOrderID: "TEST-1", SubmittedAt: time.Now()}
	}
	return f.result, nil
}

func (f *fakeAdapter) Close() error { return nil }

// submissions reports how many times an order reached the broker.
func (f *fakeAdapter) submissions() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.submitCalls
}

// openMarket is a positively open market.
func openMarket() domain.MarketStatus {
	return domain.MarketStatus{State: domain.MarketOpen, Raw: "بازار باز است", ObservedAt: time.Now()}
}

func closedMarket() domain.MarketStatus {
	return domain.MarketStatus{State: domain.MarketClosed, Raw: "بازار بسته است", ObservedAt: time.Now()}
}

func unknownMarket() domain.MarketStatus {
	return domain.UnknownMarket("???", time.Now())
}

// quoteAt builds a fresh quote with the given best ask.
func quoteAt(bestAsk int64) domain.Quote {
	return domain.Quote{
		Symbol:    "فولاد",
		LastPrice: bestAsk - 5,
		BestAsk:   bestAsk,
		BestBid:   bestAsk - 5,
		Timestamp: time.Now(),
	}
}
