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

// QuoteService resolves symbols and reads live quotes.
type QuoteService struct {
	adapter broker.Adapter
	maxAge  time.Duration
	log     *slog.Logger
	clock   Clock
}

// NewQuoteService builds the service. maxAge is the oldest a quote may be and
// still be usable.
func NewQuoteService(adapter broker.Adapter, maxAge time.Duration, log *slog.Logger, clock Clock) *QuoteService {
	if log == nil {
		log = slog.Default()
	}
	return &QuoteService{adapter: adapter, maxAge: maxAge, log: log, clock: orNow(clock)}
}

// Resolve matches the symbol exactly. Ambiguity aborts.
func (s *QuoteService) Resolve(ctx context.Context, symbol string) (domain.Symbol, error) {
	resolved, err := s.adapter.ResolveSymbol(ctx, symbol)
	if err != nil {
		return domain.Symbol{}, err
	}
	// The adapter is trusted to match exactly, and this check makes a sloppy
	// adapter fail closed rather than trade the wrong instrument.
	if !broker.SymbolsEqual(resolved.Name, symbol) {
		return domain.Symbol{}, fmt.Errorf("%w: asked for %q, broker resolved %q",
			domain.ErrSymbolAmbiguous, symbol, resolved.Name)
	}
	s.log.Info(logging.EventSymbolResolved, slog.String("symbol", resolved.Name))
	return resolved, nil
}

// Quote reads a live quote and verifies its freshness. A quote whose freshness
// cannot be established aborts.
func (s *QuoteService) Quote(ctx context.Context, sym domain.Symbol) (domain.Quote, error) {
	q, err := s.adapter.Quote(ctx, sym)
	if err != nil {
		return domain.Quote{}, err
	}
	if err := q.Validate(); err != nil {
		return domain.Quote{}, err
	}
	if !broker.SymbolsEqual(q.Symbol, sym.Name) {
		return domain.Quote{}, fmt.Errorf("%w: quote is for %q, expected %q",
			domain.ErrOrderMismatch, q.Symbol, sym.Name)
	}
	if err := EnsureFresh(q, s.clock(), s.maxAge); err != nil {
		return domain.Quote{}, err
	}
	return q, nil
}

// ResolveAndQuote is the `goroker quote` use case.
func (s *QuoteService) ResolveAndQuote(ctx context.Context, symbol string) (domain.Symbol, domain.Quote, error) {
	sym, err := s.Resolve(ctx, symbol)
	if err != nil {
		return domain.Symbol{}, domain.Quote{}, err
	}
	q, err := s.Quote(ctx, sym)
	if err != nil {
		return sym, domain.Quote{}, err
	}
	return sym, q, nil
}

// EnsureFresh rejects a quote that is older than maxAge, has no timestamp, or
// is timestamped in the future.
func EnsureFresh(q domain.Quote, at time.Time, maxAge time.Duration) error {
	if q.Timestamp.IsZero() {
		return fmt.Errorf("%w: quote has no observation timestamp", domain.ErrQuoteStale)
	}
	if maxAge <= 0 {
		return fmt.Errorf("%w: no maximum quote age configured", domain.ErrQuoteStale)
	}
	if !q.IsFresh(at, maxAge) {
		return fmt.Errorf("%w: quote is %s old, limit is %s",
			domain.ErrQuoteStale, q.Age(at).Round(time.Millisecond), maxAge)
	}
	return nil
}
