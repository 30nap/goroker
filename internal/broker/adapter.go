// Package broker defines the brokerage abstraction. Everything above this
// package (application services, commands) works only with domain types; every
// brokerage-specific detail, including all DOM selectors, lives in an adapter
// implementation under this package.
package broker

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"

	"github.com/30nap/goroker/internal/browser"
	"github.com/30nap/goroker/internal/config"
	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/storage"
)

// Adapter is the contract a brokerage implementation must satisfy.
//
// Safety contract: SubmitPreparedOrder must never be called unless the user
// explicitly confirmed the order AND final validation succeeded immediately
// before the call. Guard (below) enforces this at the adapter boundary, in
// addition to the order service's own state machine.
type Adapter interface {
	// Name identifies the adapter, e.g. for `goroker status`.
	Name() string

	// Login drives the login flow, pausing for any security challenge so the
	// user can complete it manually.
	Login(ctx context.Context) error

	// IsAuthenticated reports whether the restored session is logged in.
	// An uncertain answer must be reported as an error, not as false-positive
	// authentication.
	IsAuthenticated(ctx context.Context) (bool, error)

	// MarketStatus reads the exchange state from the broker UI.
	MarketStatus(ctx context.Context) (domain.MarketStatus, error)

	// ResolveSymbol matches a symbol exactly. Multiple matches must return
	// domain.ErrSymbolAmbiguous.
	ResolveSymbol(ctx context.Context, symbol string) (domain.Symbol, error)

	// Quote reads a live quote with an observation timestamp.
	Quote(ctx context.Context, symbol domain.Symbol) (domain.Quote, error)

	// PrepareBuyOrder fills the BUY order form. It must not submit.
	PrepareBuyOrder(ctx context.Context, req domain.BuyOrderRequest, price int64, source domain.PriceSource) (domain.PreparedOrder, error)

	// ReadPreparedOrder reads back what the order form currently contains,
	// from the live DOM, without trusting what was typed into it.
	ReadPreparedOrder(ctx context.Context) (domain.PreparedOrder, error)

	// SubmitPreparedOrder clicks submit and reads the broker's response.
	SubmitPreparedOrder(ctx context.Context) (domain.OrderResult, error)

	// Close releases adapter resources. It does not close the browser.
	Close() error
}

// Deps is what an adapter needs to be built.
type Deps struct {
	Config      config.Config
	Browser     *browser.Browser
	Credentials storage.Store
	Logger      *slog.Logger
}

// Factory builds an adapter.
type Factory func(Deps) (Adapter, error)

var (
	registryMu sync.RWMutex
	registry   = map[string]Factory{}
)

// Register adds an adapter implementation under name. It is called from
// adapter packages' init functions.
func Register(name string, f Factory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	registry[name] = f
}

// New builds the adapter named in the configuration.
func New(deps Deps) (Adapter, error) {
	name := deps.Config.Broker.Name
	registryMu.RLock()
	f, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("%w: unknown broker %q; known brokers: %v", domain.ErrAbort, name, Names())
	}
	return f(deps)
}

// Names lists the registered adapters.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
