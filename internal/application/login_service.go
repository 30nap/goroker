package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/browser"
	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/logging"
)

// LoginService drives authentication and reports application status.
type LoginService struct {
	adapter broker.Adapter
	log     *slog.Logger
}

// NewLoginService builds the service.
func NewLoginService(adapter broker.Adapter, log *slog.Logger) *LoginService {
	if log == nil {
		log = slog.Default()
	}
	return &LoginService{adapter: adapter, log: log}
}

// Login restores the session and, if that is not enough, drives the login flow.
// Any security challenge is left to the user to complete in the browser window.
func (s *LoginService) Login(ctx context.Context) error {
	ok, err := s.adapter.IsAuthenticated(ctx)
	if err == nil && ok {
		s.log.Info(logging.EventSessionRestored)
		return nil
	}
	if err != nil && !errors.Is(err, domain.ErrAuthUncertain) && !errors.Is(err, domain.ErrNotAuthenticated) {
		return err
	}
	return s.adapter.Login(ctx)
}

// AuthState is the three-valued authentication answer shown by `goroker status`.
type AuthState string

const (
	AuthAuthenticated AuthState = "authenticated"
	AuthUnauthorized  AuthState = "not authenticated"
	AuthUnknown       AuthState = "unknown"
)

// Status is what `goroker status` reports.
type Status struct {
	BrokerName string
	Auth       AuthState
	// AuthDetail carries the reason when Auth is unknown.
	AuthDetail string
	Session    browser.SessionInfo
	Market     domain.MarketStatus
	MarketErr  string
	// MissingSelectors lists broker-discovery work that is still outstanding.
	MissingSelectors []string
}

// Status gathers the current state. It never fails the whole command because
// one probe failed: each field reports its own uncertainty, and uncertainty is
// reported as such rather than as a positive answer.
func (s *LoginService) Status(ctx context.Context, session browser.SessionInfo) Status {
	out := Status{
		BrokerName: s.adapter.Name(),
		Session:    session,
		Auth:       AuthUnknown,
		Market:     domain.UnknownMarket("", now()),
	}

	authed, err := s.adapter.IsAuthenticated(ctx)
	switch {
	case err != nil:
		out.Auth = AuthUnknown
		out.AuthDetail = err.Error()
	case authed:
		out.Auth = AuthAuthenticated
	default:
		out.Auth = AuthUnauthorized
	}

	market, err := s.adapter.MarketStatus(ctx)
	if err != nil {
		out.MarketErr = err.Error()
		out.Market = domain.UnknownMarket("", now())
	} else {
		out.Market = market
	}

	if sel, ok := s.adapter.(interface {
		Selectors() interface{ Missing() []string }
	}); ok {
		out.MissingSelectors = sel.Selectors().Missing()
	}
	return out
}

// RequireAuthenticated returns an error unless the session is positively
// authenticated. An uncertain answer is an ABORT.
func RequireAuthenticated(ctx context.Context, adapter broker.Adapter) error {
	ok, err := adapter.IsAuthenticated(ctx)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("%w: run `goroker login` first", domain.ErrNotAuthenticated)
	}
	return nil
}
