package domain

import (
	"fmt"
	"sync"
)

// State is a step in the BUY lifecycle. The machine is explicit so that every
// order goes through the same sequence and no step can be skipped.
type State string

const (
	StateInitial         State = "INITIAL"
	StateAuthenticated   State = "AUTHENTICATED"
	StateMarketOpen      State = "MARKET_OPEN"
	StateSymbolResolved  State = "SYMBOL_RESOLVED"
	StateWatching        State = "WATCHING"
	StateTargetReached   State = "TARGET_REACHED"
	StateOrderPreparing  State = "ORDER_PREPARING"
	StateOrderPrepared   State = "ORDER_PREPARED"
	StateOrderValidated  State = "ORDER_VALIDATED"
	StateAwaitingConfirm State = "AWAITING_CONFIRMATION"
	StateConfirmed       State = "CONFIRMED"
	StateFinalValidation State = "FINAL_VALIDATION"
	StateSubmitting      State = "SUBMITTING"
	StateAccepted        State = "ACCEPTED"
	StateAborted         State = "ABORTED"
)

// allowedTransitions is the complete transition table. Anything not listed is
// rejected. ABORTED is reachable from every non-terminal state and is terminal
// itself.
var allowedTransitions = map[State][]State{
	StateInitial:         {StateAuthenticated},
	StateAuthenticated:   {StateMarketOpen},
	StateMarketOpen:      {StateSymbolResolved},
	StateSymbolResolved:  {StateWatching},
	StateWatching:        {StateTargetReached},
	StateTargetReached:   {StateOrderPreparing},
	StateOrderPreparing:  {StateOrderPrepared},
	StateOrderPrepared:   {StateOrderValidated},
	StateOrderValidated:  {StateAwaitingConfirm},
	StateAwaitingConfirm: {StateConfirmed},
	StateConfirmed:       {StateFinalValidation},
	// A failed final validation may return to watching instead of aborting.
	StateFinalValidation: {StateSubmitting, StateWatching},
	StateSubmitting:      {StateAccepted},
	StateAccepted:        {},
	StateAborted:         {},
}

// Machine tracks the current state of a single BUY lifecycle. It is safe for
// concurrent use because the watch loop reports progress from another goroutine.
type Machine struct {
	mu      sync.Mutex
	current State
	history []State
}

// NewMachine returns a machine in the INITIAL state.
func NewMachine() *Machine {
	return &Machine{current: StateInitial, history: []State{StateInitial}}
}

// Current returns the current state.
func (m *Machine) Current() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current
}

// History returns the states the machine has passed through, in order.
func (m *Machine) History() []State {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]State, len(m.history))
	copy(out, m.history)
	return out
}

// To performs a transition, returning ErrInvalidTransition if the move is not
// part of the lifecycle.
func (m *Machine) To(next State) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if next == StateAborted {
		if m.current == StateAccepted {
			return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, m.current, next)
		}
		m.current = StateAborted
		m.history = append(m.history, StateAborted)
		return nil
	}
	for _, allowed := range allowedTransitions[m.current] {
		if allowed == next {
			m.current = next
			m.history = append(m.history, next)
			return nil
		}
	}
	return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, m.current, next)
}

// Abort moves the machine to ABORTED. It is a no-op when already aborted.
func (m *Machine) Abort() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.current == StateAborted {
		return
	}
	m.current = StateAborted
	m.history = append(m.history, StateAborted)
}

// MaySubmit reports whether submission is permitted right now. Submission is
// only ever allowed from FINAL_VALIDATION, which is only reachable through
// CONFIRMED, which is only reachable through AWAITING_CONFIRMATION.
func (m *Machine) MaySubmit() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.current == StateFinalValidation
}
