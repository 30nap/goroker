package application_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/30nap/goroker/internal/application"
	"github.com/30nap/goroker/internal/domain"
)

// discardLogger keeps test output readable.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// stubConfirmer answers the confirmation prompt with a fixed outcome and
// records whether it was asked at all.
type stubConfirmer struct {
	err    error
	asked  int
	before func()
}

func (s *stubConfirmer) Confirm(ctx context.Context, _ application.ConfirmationPrompt) error {
	s.asked++
	if s.before != nil {
		s.before()
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.err
}

func rangeRequest() domain.BuyOrderRequest {
	return domain.BuyOrderRequest{Symbol: "فولاد", MinPrice: 3250, MaxPrice: 3350, Quantity: 10000}
}

func testOptions() application.OrderOptions {
	return application.OrderOptions{
		WatchInterval:       time.Millisecond,
		MaxQuoteAge:         5 * time.Second,
		ConfirmationTimeout: time.Second,
		SubmissionEnabled:   true,
	}
}

func newService(t *testing.T, adapter *fakeAdapter, confirmer application.Confirmer, opts application.OrderOptions) *application.OrderService {
	t.Helper()
	return application.NewOrderService(adapter, confirmer, opts, discardLogger(), nil)
}

// --- Safety test 1 ---------------------------------------------------------

// TestNoSubmissionWithoutConfirmation proves that a refused confirmation stops
// the order: the broker is never asked to submit.
func TestNoSubmissionWithoutConfirmation(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
	}
	confirmer := &stubConfirmer{err: domain.ErrNotConfirmed}
	svc := newService(t, adapter, confirmer, testOptions())

	outcome, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if !errors.Is(err, domain.ErrNotConfirmed) {
		t.Fatalf("Buy() = %v, want ErrNotConfirmed", err)
	}
	if confirmer.asked != 1 {
		t.Fatalf("confirmation asked %d times, want 1", confirmer.asked)
	}
	if adapter.submissions() != 0 {
		t.Fatalf("broker was asked to submit %d times, want 0", adapter.submissions())
	}
	if outcome.Submitted {
		t.Fatal("outcome reports a submission that never happened")
	}
}

// TestNoConfirmerMeansNoSubmission covers the wiring mistake of building the
// service without a confirmation prompt at all.
func TestNoConfirmerMeansNoSubmission(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
	}
	svc := newService(t, adapter, nil, testOptions())

	if _, err := svc.Buy(context.Background(), rangeRequest(), nil); !errors.Is(err, domain.ErrNotConfirmed) {
		t.Fatalf("Buy() without a confirmer = %v, want ErrNotConfirmed", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted without a confirmation prompt")
	}
}

// --- Safety test 2 ---------------------------------------------------------

// TestDOMMismatchAborts proves that any difference between the expected order
// and the order read back from the page aborts before confirmation.
func TestDOMMismatchAborts(t *testing.T) {
	cases := []struct {
		name string
		dom  domain.PreparedOrder
		want error
	}{
		{
			name: "price differs",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideBuy, Price: 3400, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "quantity differs",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideBuy, Price: 3300, Quantity: 1},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "symbol differs",
			dom:  domain.PreparedOrder{Symbol: "خودرو", Side: domain.SideBuy, Price: 3300, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "side differs",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideSell, Price: 3300, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "side is unreadable",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Price: 3300, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "broker cost estimate disagrees with price times quantity",
			dom: domain.PreparedOrder{
				Symbol: "فولاد", Side: domain.SideBuy, Price: 3300, Quantity: 10000,
				EstimatedCost: 1,
			},
			want: domain.ErrOrderMismatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &fakeAdapter{
				authenticated: true,
				market:        openMarket(),
				quotes:        []domain.Quote{quoteAt(3300)},
				domOrders:     []domain.PreparedOrder{tc.dom},
			}
			confirmer := &stubConfirmer{}
			svc := newService(t, adapter, confirmer, testOptions())

			_, err := svc.Buy(context.Background(), rangeRequest(), nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Buy() = %v, want %v", err, tc.want)
			}
			if confirmer.asked != 0 {
				t.Fatal("the user was asked to confirm an order that did not match the page")
			}
			if adapter.submissions() != 0 {
				t.Fatal("an order was submitted despite a DOM mismatch")
			}
		})
	}
}

// TestFormChangedAfterConfirmationAborts covers the page changing between the
// confirmation and the click.
func TestFormChangedAfterConfirmationAborts(t *testing.T) {
	good := domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideBuy, Price: 3300, Quantity: 10000}
	tampered := domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideBuy, Price: 3300, Quantity: 99999}

	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
		domOrders:     []domain.PreparedOrder{good, tampered},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	_, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if !errors.Is(err, domain.ErrOrderMismatch) {
		t.Fatalf("Buy() = %v, want ErrOrderMismatch", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted after the form changed under us")
	}
}

// --- Safety test 3 ---------------------------------------------------------

// TestPriceLeavingRangeAfterConfirmationAborts proves that a price which moves
// outside the requested range between confirmation and submission stops the order.
func TestPriceLeavingRangeAfterConfirmationAborts(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes: []domain.Quote{
			quoteAt(3300), // watch: in range, order is prepared at 3300
			quoteAt(3400), // final validation: the market ran away
		},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	_, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if !errors.Is(err, domain.ErrPriceOutOfRange) {
		t.Fatalf("Buy() = %v, want ErrPriceOutOfRange", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted at a price outside the requested range")
	}
}

// TestWatchWaitsWhilePriceIsOutsideRange proves the order is not prepared at
// all while the price is above the range; the run ends by cancellation, not by
// trading.
func TestWatchWaitsWhilePriceIsOutsideRange(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(4000)}, // always above the range
	}
	confirmer := &stubConfirmer{}
	svc := newService(t, adapter, confirmer, testOptions())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := svc.Buy(ctx, rangeRequest(), nil)
	if !errors.Is(err, domain.ErrCancelled) {
		t.Fatalf("Buy() = %v, want ErrCancelled", err)
	}
	if confirmer.asked != 0 {
		t.Fatal("the user was asked to confirm while the price was out of range")
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted while the price was out of range")
	}
}

// TestBelowRangeIsNotTraded covers a price under the range: still no order.
func TestBelowRangeIsNotTraded(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3000)},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	if _, err := svc.Buy(ctx, rangeRequest(), nil); !errors.Is(err, domain.ErrCancelled) {
		t.Fatalf("Buy() = %v, want ErrCancelled", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted below the requested range")
	}
}

// --- Safety test 4 ---------------------------------------------------------

// TestMarketStateBlocksSubmission proves that a market which is not positively
// open — closed or unknown — prevents any order.
func TestMarketStateBlocksSubmission(t *testing.T) {
	cases := []struct {
		name   string
		status domain.MarketStatus
		want   error
	}{
		{"unknown market", unknownMarket(), domain.ErrMarketUnknown},
		{"closed market", closedMarket(), domain.ErrMarketNotOpen},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			adapter := &fakeAdapter{
				authenticated: true,
				market:        tc.status,
				quotes:        []domain.Quote{quoteAt(3300)},
			}
			confirmer := &stubConfirmer{}
			svc := newService(t, adapter, confirmer, testOptions())

			_, err := svc.Buy(context.Background(), rangeRequest(), nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Buy() = %v, want %v", err, tc.want)
			}
			if confirmer.asked != 0 || adapter.submissions() != 0 {
				t.Fatal("the flow continued although the market was not open")
			}
		})
	}
}

// TestMarketClosingAfterConfirmationAborts covers the market going unknown
// between the confirmation and the click.
func TestMarketClosingAfterConfirmationAborts(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		markets: []domain.MarketStatus{
			openMarket(),    // initial check
			openMarket(),    // watch tick
			unknownMarket(), // final validation
		},
		quotes: []domain.Quote{quoteAt(3300)},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	_, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if !errors.Is(err, domain.ErrMarketUnknown) {
		t.Fatalf("Buy() = %v, want ErrMarketUnknown", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted into a market of unknown state")
	}
}

// --- Safety test 6 ---------------------------------------------------------

// TestDryRunNeverSubmits proves a dry run stops after validation, even when the
// user would have confirmed.
func TestDryRunNeverSubmits(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
	}
	confirmer := &stubConfirmer{}
	opts := testOptions()
	opts.DryRun = true
	svc := newService(t, adapter, confirmer, opts)

	outcome, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if err != nil {
		t.Fatalf("Buy() = %v, want nil", err)
	}
	if outcome.Submitted {
		t.Fatal("dry run reported a submission")
	}
	if adapter.submissions() != 0 {
		t.Fatal("dry run submitted an order")
	}
	if confirmer.asked != 0 {
		t.Fatal("dry run asked for a confirmation it could not act on")
	}
	if outcome.Prepared.Price != 3300 || outcome.Prepared.Quantity != 10000 {
		t.Fatalf("dry run prepared %+v, want price 3300 quantity 10000", outcome.Prepared)
	}
}

// --- other fail-closed conditions -----------------------------------------

func TestBuyFailsClosed(t *testing.T) {
	cases := []struct {
		name    string
		adapter *fakeAdapter
		want    error
	}{
		{
			name:    "not authenticated",
			adapter: &fakeAdapter{authenticated: false, market: openMarket(), quotes: []domain.Quote{quoteAt(3300)}},
			want:    domain.ErrNotAuthenticated,
		},
		{
			name:    "authentication uncertain",
			adapter: &fakeAdapter{authErr: domain.ErrAuthUncertain, market: openMarket()},
			want:    domain.ErrAuthUncertain,
		},
		{
			name:    "browser disconnected",
			adapter: &fakeAdapter{authErr: domain.ErrBrowserUnavailable},
			want:    domain.ErrBrowserUnavailable,
		},
		{
			name: "ambiguous symbol",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				resolveErr: domain.ErrSymbolAmbiguous,
			},
			want: domain.ErrSymbolAmbiguous,
		},
		{
			name: "symbol not found",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				resolveErr: domain.ErrSymbolNotFound,
			},
			want: domain.ErrSymbolNotFound,
		},
		{
			name: "broker resolved a different symbol",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				symbol: domain.Symbol{Name: "خودرو"},
				quotes: []domain.Quote{quoteAt(3300)},
			},
			want: domain.ErrSymbolAmbiguous,
		},
		{
			name: "stale quote",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				quotes: []domain.Quote{{
					Symbol: "فولاد", LastPrice: 3300, BestAsk: 3300, BestBid: 3295,
					Timestamp: time.Now().Add(-time.Minute),
				}},
			},
			want: domain.ErrQuoteStale,
		},
		{
			name: "quote without a timestamp cannot be shown to be fresh",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				quoteErr: domain.ErrQuoteInvalid,
			},
			want: domain.ErrQuoteInvalid,
		},
		{
			name: "selector missing while preparing",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				quotes:     []domain.Quote{quoteAt(3300)},
				prepareErr: domain.ErrSelectorMissing,
			},
			want: domain.ErrSelectorMissing,
		},
		{
			name: "unexpected modal while reading the form back",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				quotes:  []domain.Quote{quoteAt(3300)},
				readErr: domain.ErrUnexpectedModal,
			},
			want: domain.ErrUnexpectedModal,
		},
		{
			name: "broker DOM changed while reading the form back",
			adapter: &fakeAdapter{
				authenticated: true, market: openMarket(),
				quotes:  []domain.Quote{quoteAt(3300)},
				readErr: domain.ErrDOMChanged,
			},
			want: domain.ErrDOMChanged,
		},
		{
			name: "market probe fails",
			adapter: &fakeAdapter{
				authenticated: true,
				marketErr:     domain.ErrBrowserUnavailable,
			},
			want: domain.ErrBrowserUnavailable,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			confirmer := &stubConfirmer{}
			svc := newService(t, tc.adapter, confirmer, testOptions())

			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()

			_, err := svc.Buy(ctx, rangeRequest(), nil)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Buy() = %v, want %v", err, tc.want)
			}
			if confirmer.asked != 0 {
				t.Fatal("a confirmation was requested although the flow should have aborted earlier")
			}
			if tc.adapter.submissions() != 0 {
				t.Fatal("an order was submitted in a fail-closed scenario")
			}
		})
	}
}

// TestConfirmationTimeoutAborts proves an expired confirmation does not submit.
func TestConfirmationTimeoutAborts(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
	}
	confirmer := &stubConfirmer{err: domain.ErrConfirmationExpire}
	svc := newService(t, adapter, confirmer, testOptions())

	_, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if !errors.Is(err, domain.ErrConfirmationExpire) {
		t.Fatalf("Buy() = %v, want ErrConfirmationExpire", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted after the confirmation expired")
	}
}

// TestCancellationDuringConfirmationAborts covers Ctrl+C pressed while the
// confirmation prompt is open.
func TestCancellationDuringConfirmationAborts(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
	}
	ctx, cancel := context.WithCancel(context.Background())
	confirmer := &stubConfirmer{before: cancel}
	svc := newService(t, adapter, confirmer, testOptions())

	_, err := svc.Buy(ctx, rangeRequest(), nil)
	if err == nil {
		t.Fatal("Buy() = nil, want an error after cancellation")
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted after Ctrl+C")
	}
}

// TestCancellationDuringWatchStops covers Ctrl+C while monitoring prices.
func TestCancellationDuringWatchStops(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(4000)},
		onQuote: func(call int) {
			if call >= 1 {
				cancel()
			}
		},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	_, err := svc.Buy(ctx, rangeRequest(), nil)
	if !errors.Is(err, domain.ErrCancelled) {
		t.Fatalf("Buy() = %v, want ErrCancelled", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted after cancellation")
	}
}

// --- happy paths -----------------------------------------------------------

// TestConfirmedOrderIsSubmitted is the positive control: with everything in
// order and an explicit confirmation, exactly one order is submitted.
func TestConfirmedOrderIsSubmitted(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	outcome, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if err != nil {
		t.Fatalf("Buy() = %v, want nil", err)
	}
	if !outcome.Submitted || adapter.submissions() != 1 {
		t.Fatalf("submitted=%v submissions=%d, want true and 1", outcome.Submitted, adapter.submissions())
	}
	if outcome.Result.Status != domain.OrderAccepted {
		t.Fatalf("status = %s, want ACCEPTED", outcome.Result.Status)
	}
	if outcome.Prepared.Price != 3300 {
		t.Fatalf("order price = %d, want the observed best ask 3300", outcome.Prepared.Price)
	}
	last := outcome.States[len(outcome.States)-1]
	if last != domain.StateAccepted {
		t.Fatalf("final state = %s, want ACCEPTED", last)
	}
}

// TestSubmissionDisabledStopsBeforeBroker proves the build-level switch can
// only make Goroker safer: with it off, nothing is submitted even after a
// confirmation.
func TestSubmissionDisabledStopsBeforeBroker(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
	}
	opts := testOptions()
	opts.SubmissionEnabled = false
	svc := newService(t, adapter, &stubConfirmer{}, opts)

	_, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if !errors.Is(err, domain.ErrNotConfirmed) {
		t.Fatalf("Buy() = %v, want ErrNotConfirmed", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an order was submitted although submission is disabled in this build")
	}
}

// TestExactPriceIsNeverAltered proves exact-price mode uses the requested price
// and waits for the market to come to it.
func TestExactPriceIsNeverAltered(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes: []domain.Quote{
			quoteAt(3320), // not the requested price yet: keep waiting
			quoteAt(3310), // now the best ask is exactly the requested price
		},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	req := domain.BuyOrderRequest{Symbol: "فولاد", Price: 3310, Quantity: 10000}
	outcome, err := svc.Buy(context.Background(), req, nil)
	if err != nil {
		t.Fatalf("Buy() = %v, want nil", err)
	}
	if outcome.Prepared.Price != 3310 {
		t.Fatalf("order price = %d, want exactly the requested 3310", outcome.Prepared.Price)
	}
	if adapter.submissions() != 1 {
		t.Fatalf("submissions = %d, want 1", adapter.submissions())
	}
}

// TestInvalidRequestNeverTouchesTheBroker keeps bad input away from the browser.
func TestInvalidRequestNeverTouchesTheBroker(t *testing.T) {
	adapter := &fakeAdapter{authenticated: true, market: openMarket()}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	req := domain.BuyOrderRequest{Symbol: "فولاد", MinPrice: 3350, MaxPrice: 3250, Quantity: 10}
	if _, err := svc.Buy(context.Background(), req, nil); !errors.Is(err, domain.ErrAbort) {
		t.Fatalf("Buy() = %v, want ErrAbort", err)
	}
	if adapter.submissions() != 0 {
		t.Fatal("an invalid request reached the broker")
	}
}

// TestRejectedBrokerResponseIsNotSuccess proves an order the broker rejects is
// reported as a failure.
func TestRejectedBrokerResponseIsNotSuccess(t *testing.T) {
	adapter := &fakeAdapter{
		authenticated: true,
		market:        openMarket(),
		quotes:        []domain.Quote{quoteAt(3300)},
		result: domain.OrderResult{
			Status:  domain.OrderRejected,
			Message: "اعتبار کافی نیست",
		},
	}
	svc := newService(t, adapter, &stubConfirmer{}, testOptions())

	outcome, err := svc.Buy(context.Background(), rangeRequest(), nil)
	if err == nil {
		t.Fatal("Buy() = nil, want an error for a rejected order")
	}
	if !strings.Contains(err.Error(), "REJECTED") {
		t.Fatalf("error %q does not mention the broker status", err)
	}
	if !outcome.Submitted {
		t.Fatal("outcome should record that an order was sent")
	}
}
