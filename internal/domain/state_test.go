package domain_test

import (
	"errors"
	"testing"

	"github.com/30nap/goroker/internal/domain"
)

// happyPath is the only sequence that reaches submission.
var happyPath = []domain.State{
	domain.StateAuthenticated,
	domain.StateMarketOpen,
	domain.StateSymbolResolved,
	domain.StateWatching,
	domain.StateTargetReached,
	domain.StateOrderPreparing,
	domain.StateOrderPrepared,
	domain.StateOrderValidated,
	domain.StateAwaitingConfirm,
	domain.StateConfirmed,
	domain.StateFinalValidation,
	domain.StateSubmitting,
	domain.StateAccepted,
}

func TestMachineHappyPath(t *testing.T) {
	m := domain.NewMachine()
	for _, next := range happyPath {
		if err := m.To(next); err != nil {
			t.Fatalf("To(%s) = %v, want nil", next, err)
		}
	}
	if m.Current() != domain.StateAccepted {
		t.Fatalf("final state = %s, want %s", m.Current(), domain.StateAccepted)
	}
}

func TestMachineRejectsSkippedSteps(t *testing.T) {
	// Jumping straight from INITIAL to submission must be refused.
	m := domain.NewMachine()
	if err := m.To(domain.StateSubmitting); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("To(SUBMITTING) from INITIAL = %v, want ErrInvalidTransition", err)
	}

	// Skipping confirmation must be refused too.
	m = domain.NewMachine()
	for _, next := range happyPath[:8] { // up to ORDER_VALIDATED
		if err := m.To(next); err != nil {
			t.Fatalf("setup To(%s) = %v", next, err)
		}
	}
	if err := m.To(domain.StateConfirmed); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("To(CONFIRMED) without AWAITING_CONFIRMATION = %v, want ErrInvalidTransition", err)
	}
	if err := m.To(domain.StateFinalValidation); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("To(FINAL_VALIDATION) without CONFIRMED = %v, want ErrInvalidTransition", err)
	}
}

func TestMachineMaySubmitOnlyAfterFinalValidation(t *testing.T) {
	m := domain.NewMachine()
	for _, next := range happyPath {
		if next == domain.StateSubmitting {
			break
		}
		if err := m.To(next); err != nil {
			t.Fatalf("To(%s) = %v", next, err)
		}
		want := next == domain.StateFinalValidation
		if got := m.MaySubmit(); got != want {
			t.Fatalf("MaySubmit() in state %s = %v, want %v", next, got, want)
		}
	}
}

func TestMachineAbortIsTerminal(t *testing.T) {
	m := domain.NewMachine()
	if err := m.To(domain.StateAuthenticated); err != nil {
		t.Fatalf("To(AUTHENTICATED) = %v", err)
	}
	m.Abort()
	if m.Current() != domain.StateAborted {
		t.Fatalf("Current() = %s, want ABORTED", m.Current())
	}
	if m.MaySubmit() {
		t.Fatal("MaySubmit() is true after abort")
	}
	if err := m.To(domain.StateMarketOpen); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("To(MARKET_OPEN) after abort = %v, want ErrInvalidTransition", err)
	}
}

func TestMachineFinalValidationMayReturnToWatching(t *testing.T) {
	m := domain.NewMachine()
	for _, next := range happyPath {
		if err := m.To(next); err != nil {
			t.Fatalf("To(%s) = %v", next, err)
		}
		if next == domain.StateFinalValidation {
			break
		}
	}
	if err := m.To(domain.StateWatching); err != nil {
		t.Fatalf("FINAL_VALIDATION -> WATCHING = %v, want nil", err)
	}
	if m.MaySubmit() {
		t.Fatal("MaySubmit() is true after returning to WATCHING")
	}
}
