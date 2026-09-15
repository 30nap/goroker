package application

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/logging"
)

// Target is the price window a watch waits for, and the quote field it is
// measured against. The field is explicit so Goroker never silently switches
// between price definitions.
type Target struct {
	MinPrice int64
	MaxPrice int64
	Source   domain.PriceSource
}

// Contains reports whether price is inside the inclusive target window.
func (t Target) Contains(price int64) bool {
	return price >= t.MinPrice && price <= t.MaxPrice
}

// Observation is one reading taken by the watch loop.
type Observation struct {
	Quote   domain.Quote
	Price   int64
	Market  domain.MarketStatus
	InRange bool
}

// WatchService monitors a live quote until the target price window is reached.
// It never prepares or submits an order.
type WatchService struct {
	adapter  broker.Adapter
	interval time.Duration
	maxAge   time.Duration
	log      *slog.Logger
	clock    Clock
	// RequireOpenMarket makes a non-open market abort the watch. `goroker buy`
	// sets it; `goroker watch` also sets it, because a closed market cannot
	// produce a tradable quote.
	RequireOpenMarket bool
}

// NewWatchService builds the service.
func NewWatchService(adapter broker.Adapter, interval, maxAge time.Duration, log *slog.Logger, clock Clock) *WatchService {
	if log == nil {
		log = slog.Default()
	}
	if interval <= 0 {
		interval = 2 * time.Second
	}
	return &WatchService{
		adapter:           adapter,
		interval:          interval,
		maxAge:            maxAge,
		log:               log,
		clock:             orNow(clock),
		RequireOpenMarket: true,
	}
}

// Interval is the configured polling interval.
func (s *WatchService) Interval() time.Duration { return s.interval }

// Wait polls the live quote until the target window is reached, calling onTick
// for every observation. It returns the observation that reached the target.
//
// Every tick re-checks the market state: a market that closes or becomes
// unknown mid-watch aborts rather than continuing to wait on stale prices.
func (s *WatchService) Wait(ctx context.Context, sym domain.Symbol, target Target, onTick func(Observation)) (Observation, error) {
	quotes := NewQuoteService(s.adapter, s.maxAge, s.log, s.clock)

	s.log.Info(logging.EventWatchingPrice,
		slog.String("symbol", sym.Name),
		slog.Int64("min_price", target.MinPrice),
		slog.Int64("max_price", target.MaxPrice),
		slog.String("price_source", string(target.Source)),
		slog.Duration("interval", s.interval))

	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	for {
		obs, err := s.observe(ctx, quotes, sym, target)
		if err != nil {
			return Observation{}, err
		}
		if onTick != nil {
			onTick(obs)
		}
		if obs.InRange {
			s.log.Info(logging.EventTargetPriceReache,
				slog.String("symbol", sym.Name),
				slog.Int64("price", obs.Price),
				slog.String("price_source", string(target.Source)))
			return obs, nil
		}

		select {
		case <-ctx.Done():
			return Observation{}, fmt.Errorf("%w: %v", domain.ErrCancelled, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Observe takes a single reading: market status, live quote, and whether the
// chosen price field is inside the target window.
func (s *WatchService) Observe(ctx context.Context, sym domain.Symbol, target Target) (Observation, error) {
	return s.observe(ctx, NewQuoteService(s.adapter, s.maxAge, s.log, s.clock), sym, target)
}

func (s *WatchService) observe(ctx context.Context, quotes *QuoteService, sym domain.Symbol, target Target) (Observation, error) {
	if err := ctx.Err(); err != nil {
		return Observation{}, fmt.Errorf("%w: %v", domain.ErrCancelled, err)
	}

	market, err := s.adapter.MarketStatus(ctx)
	if err != nil {
		return Observation{}, err
	}
	if s.RequireOpenMarket {
		if err := RequireOpenMarket(market); err != nil {
			return Observation{}, err
		}
	}

	q, err := quotes.Quote(ctx, sym)
	if err != nil {
		return Observation{}, err
	}
	price, err := q.Price(target.Source)
	if err != nil {
		return Observation{}, err
	}
	if price <= 0 {
		return Observation{}, fmt.Errorf("%w: %s is %d", domain.ErrQuoteInvalid, target.Source, price)
	}

	return Observation{
		Quote:   q,
		Price:   price,
		Market:  market,
		InRange: target.Contains(price),
	}, nil
}

// RequireOpenMarket turns a non-open market into an error. UNKNOWN is never
// treated as open.
func RequireOpenMarket(status domain.MarketStatus) error {
	switch status.State {
	case domain.MarketOpen:
		return nil
	case domain.MarketClosed:
		return fmt.Errorf("%w: broker reports %q", domain.ErrMarketNotOpen, status.Raw)
	default:
		return fmt.Errorf("%w: broker state could not be established (raw: %q)", domain.ErrMarketUnknown, status.Raw)
	}
}
