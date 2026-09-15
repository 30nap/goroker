package broker

import (
	"context"
	"fmt"

	"github.com/30nap/goroker/internal/domain"
)

// SubmitAuthorizer answers, at the instant of the call, whether an order may
// be submitted. Implementations return a non-nil error unless the user has
// explicitly confirmed and final validation has just succeeded.
type SubmitAuthorizer interface {
	AuthorizeSubmit() error
}

// AuthorizerFunc adapts a function to SubmitAuthorizer.
type AuthorizerFunc func() error

// AuthorizeSubmit implements SubmitAuthorizer.
func (f AuthorizerFunc) AuthorizeSubmit() error { return f() }

// DenyAll refuses every submission. It is the default authorizer, so an
// adapter that is wired up without an explicit authorizer cannot submit.
var DenyAll SubmitAuthorizer = AuthorizerFunc(func() error {
	return fmt.Errorf("%w: submission is not authorized", domain.ErrNotConfirmed)
})

// Guard wraps an Adapter and blocks SubmitPreparedOrder unless the authorizer
// permits it. This is defence in depth: the order service already refuses to
// reach submission without confirmation, and this makes a mistake in any
// future caller fail closed rather than place an order.
type Guard struct {
	Adapter
	authorizer SubmitAuthorizer
}

// NewGuard wraps inner. A nil authorizer means DenyAll.
func NewGuard(inner Adapter, authorizer SubmitAuthorizer) *Guard {
	if authorizer == nil {
		authorizer = DenyAll
	}
	return &Guard{Adapter: inner, authorizer: authorizer}
}

// SubmitPreparedOrder submits only when the authorizer agrees.
func (g *Guard) SubmitPreparedOrder(ctx context.Context) (domain.OrderResult, error) {
	if err := g.authorizer.AuthorizeSubmit(); err != nil {
		return domain.OrderResult{}, err
	}
	if err := ctx.Err(); err != nil {
		// Cancellation between confirmation and the click must abort.
		return domain.OrderResult{}, fmt.Errorf("%w: %v", domain.ErrCancelled, err)
	}
	return g.Adapter.SubmitPreparedOrder(ctx)
}
