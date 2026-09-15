// Package application contains Goroker's use cases. It depends on the domain
// and on the broker abstraction, never on a concrete brokerage or on DOM
// selectors.
package application

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/30nap/goroker/internal/domain"
)

// ConfirmWord is the only input that approves an order. Anything else aborts.
const ConfirmWord = "BUY"

// ConfirmationPrompt is everything the user is shown before approving.
type ConfirmationPrompt struct {
	Order          domain.PreparedOrder
	CurrentBestAsk int64
	MinPrice       int64
	MaxPrice       int64
	QuoteAge       time.Duration
	Timeout        time.Duration
}

// Confirmer asks the user to approve an order. Implementations must require a
// deliberate, interactive act; there is no flag and no configuration value that
// can stand in for it.
type Confirmer interface {
	// Confirm returns nil only when the user typed the confirmation word
	// within the timeout. It returns domain.ErrConfirmationExpire on timeout,
	// domain.ErrNotConfirmed on any other input, and domain.ErrCancelled when
	// the context is cancelled.
	Confirm(ctx context.Context, prompt ConfirmationPrompt) error
}

// TerminalConfirmer reads the confirmation word from a terminal.
type TerminalConfirmer struct {
	In  io.Reader
	Out io.Writer
}

// NewTerminalConfirmer returns a confirmer reading from in and writing to out.
func NewTerminalConfirmer(in io.Reader, out io.Writer) *TerminalConfirmer {
	return &TerminalConfirmer{In: in, Out: out}
}

// Confirm implements Confirmer.
func (c *TerminalConfirmer) Confirm(ctx context.Context, prompt ConfirmationPrompt) error {
	fmt.Fprint(c.Out, RenderConfirmation(prompt))

	type result struct {
		line string
		err  error
	}
	lines := make(chan result, 1)
	go func() {
		reader := bufio.NewReader(c.In)
		line, err := reader.ReadString('\n')
		lines <- result{line: line, err: err}
	}()

	timeout := prompt.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		fmt.Fprintln(c.Out, "\nCancelled. No order was submitted.")
		return fmt.Errorf("%w: %v", domain.ErrCancelled, ctx.Err())
	case <-timer.C:
		fmt.Fprintf(c.Out, "\nConfirmation expired after %s. No order was submitted.\n", timeout)
		return fmt.Errorf("%w after %s", domain.ErrConfirmationExpire, timeout)
	case res := <-lines:
		if res.err != nil && strings.TrimSpace(res.line) == "" {
			return fmt.Errorf("%w: could not read confirmation: %v", domain.ErrNotConfirmed, res.err)
		}
		// The word must match exactly, including case. Whitespace around it is
		// the only thing forgiven, because terminals add a newline.
		if strings.TrimSpace(res.line) != ConfirmWord {
			fmt.Fprintln(c.Out, "Not confirmed. No order was submitted.")
			return fmt.Errorf("%w: expected %q", domain.ErrNotConfirmed, ConfirmWord)
		}
		return nil
	}
}

// RenderConfirmation formats the confirmation screen.
func RenderConfirmation(p ConfirmationPrompt) string {
	var b strings.Builder
	b.WriteString("\n================================\n")
	b.WriteString("GOROKER ORDER CONFIRMATION\n")
	b.WriteString("================================\n\n")
	fmt.Fprintf(&b, "Symbol: %s\n", p.Order.Symbol)
	fmt.Fprintf(&b, "Side: %s\n", p.Order.Side)
	fmt.Fprintf(&b, "Quantity: %s\n", Thousands(p.Order.Quantity))
	fmt.Fprintf(&b, "Price: %s\n", Thousands(p.Order.Price))
	fmt.Fprintf(&b, "Price source: %s\n", p.Order.PriceSource)
	fmt.Fprintf(&b, "Estimated Value: %s\n", Thousands(p.Order.ExpectedCost()))
	fmt.Fprintf(&b, "Current Best Ask: %s\n", Thousands(p.CurrentBestAsk))
	fmt.Fprintf(&b, "Allowed range: %s .. %s\n", Thousands(p.MinPrice), Thousands(p.MaxPrice))
	fmt.Fprintf(&b, "Quote age: %s\n", p.QuoteAge.Round(time.Millisecond))
	fmt.Fprintf(&b, "\nThis submits a real order to your brokerage account.\n")
	fmt.Fprintf(&b, "Confirmation expires in %s.\n\n", p.Timeout)
	fmt.Fprintf(&b, "Type %s to submit: ", ConfirmWord)
	return b.String()
}

// Thousands formats an integer with thousands separators.
func Thousands(v int64) string {
	sign := ""
	if v < 0 {
		sign = "-"
		v = -v
	}
	digits := fmt.Sprintf("%d", v)
	var out strings.Builder
	for i, r := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			out.WriteByte(',')
		}
		out.WriteRune(r)
	}
	return sign + out.String()
}
