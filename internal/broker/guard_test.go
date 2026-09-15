package broker_test

import (
	"context"
	"errors"
	"testing"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/domain"
)

// countingAdapter records submissions and does nothing else.
type countingAdapter struct {
	broker.Adapter
	submits int
}

func (c *countingAdapter) SubmitPreparedOrder(context.Context) (domain.OrderResult, error) {
	c.submits++
	return domain.OrderResult{Status: domain.OrderAccepted}, nil
}

// TestGuardDeniesByDefault proves that wiring an adapter without an explicit
// authorizer cannot submit an order.
func TestGuardDeniesByDefault(t *testing.T) {
	inner := &countingAdapter{}
	guard := broker.NewGuard(inner, nil)

	if _, err := guard.SubmitPreparedOrder(context.Background()); !errors.Is(err, domain.ErrNotConfirmed) {
		t.Fatalf("SubmitPreparedOrder() = %v, want ErrNotConfirmed", err)
	}
	if inner.submits != 0 {
		t.Fatal("the guard let an unauthorized submission through")
	}
}

func TestGuardRefusesWhenAuthorizerSaysNo(t *testing.T) {
	inner := &countingAdapter{}
	guard := broker.NewGuard(inner, broker.AuthorizerFunc(func() error { return domain.ErrDryRun }))

	if _, err := guard.SubmitPreparedOrder(context.Background()); !errors.Is(err, domain.ErrDryRun) {
		t.Fatalf("SubmitPreparedOrder() = %v, want ErrDryRun", err)
	}
	if inner.submits != 0 {
		t.Fatal("the guard submitted an order during a dry run")
	}
}

func TestGuardRefusesAfterCancellation(t *testing.T) {
	inner := &countingAdapter{}
	guard := broker.NewGuard(inner, broker.AuthorizerFunc(func() error { return nil }))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := guard.SubmitPreparedOrder(ctx); !errors.Is(err, domain.ErrCancelled) {
		t.Fatalf("SubmitPreparedOrder() = %v, want ErrCancelled", err)
	}
	if inner.submits != 0 {
		t.Fatal("the guard submitted an order after cancellation")
	}
}

func TestGuardAllowsAnAuthorizedSubmission(t *testing.T) {
	inner := &countingAdapter{}
	guard := broker.NewGuard(inner, broker.AuthorizerFunc(func() error { return nil }))

	result, err := guard.SubmitPreparedOrder(context.Background())
	if err != nil {
		t.Fatalf("SubmitPreparedOrder() = %v, want nil", err)
	}
	if result.Status != domain.OrderAccepted || inner.submits != 1 {
		t.Fatalf("status=%s submits=%d, want ACCEPTED and 1", result.Status, inner.submits)
	}
}
