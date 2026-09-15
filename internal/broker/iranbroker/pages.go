package iranbroker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/go-rod/rod"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/domain"
)

// pageCtx couples a live Rod page with the selector set. All DOM access in
// this adapter goes through it, so there is exactly one place where a missing
// element turns into an ABORT.
type pageCtx struct {
	page *rod.Page
	sel  Selectors
	log  *slog.Logger
	// wait is how long to wait for an element before treating it as missing.
	wait time.Duration
}

func newPageCtx(p *rod.Page, sel Selectors, log *slog.Logger, wait time.Duration) *pageCtx {
	if wait <= 0 {
		wait = 10 * time.Second
	}
	return &pageCtx{page: p, sel: sel, log: log, wait: wait}
}

// element resolves a required selector to a single element. An empty selector,
// a missing element or more than one match is an ABORT: Goroker never acts on
// an element it cannot identify unambiguously.
func (p *pageCtx) element(ctx context.Context, name, selector string) (*rod.Element, error) {
	sel, err := Require(name, selector)
	if err != nil {
		return nil, err
	}
	el, err := p.page.Context(ctx).Timeout(p.wait).Element(sel)
	if err != nil {
		return nil, fmt.Errorf("%w: %s (%s): %v", domain.ErrSelectorMissing, name, sel, err)
	}
	return el.CancelTimeout(), nil
}

// optionalElement resolves a selector that the broker UI may legitimately not
// show. An unconfigured selector yields (nil, false, nil) so callers can carry
// on without it; a configured-but-absent element is likewise not an error.
func (p *pageCtx) optionalElement(ctx context.Context, selector string, wait time.Duration) (*rod.Element, bool, error) {
	if strings.TrimSpace(selector) == "" {
		return nil, false, nil
	}
	if wait <= 0 {
		wait = time.Second
	}
	el, err := p.page.Context(ctx).Timeout(wait).Element(selector)
	if err != nil {
		var notFound *rod.ElementNotFoundError
		if errors.As(err, &notFound) || errors.Is(err, context.DeadlineExceeded) {
			return nil, false, nil
		}
		if errors.Is(err, context.Canceled) {
			return nil, false, fmt.Errorf("%w: %v", domain.ErrCancelled, err)
		}
		return nil, false, nil
	}
	return el.CancelTimeout(), true, nil
}

// text reads the trimmed text of a required element.
func (p *pageCtx) text(ctx context.Context, name, selector string) (string, error) {
	el, err := p.element(ctx, name, selector)
	if err != nil {
		return "", err
	}
	txt, err := el.Text()
	if err != nil {
		return "", fmt.Errorf("%w: read text of %s: %v", domain.ErrDOMChanged, name, err)
	}
	return strings.TrimSpace(txt), nil
}

// number reads a required element and parses it as an integer price/quantity.
func (p *pageCtx) number(ctx context.Context, name, selector string) (int64, error) {
	txt, err := p.text(ctx, name, selector)
	if err != nil {
		return 0, err
	}
	value, err := broker.ParsePrice(txt)
	if err != nil {
		return 0, fmt.Errorf("%w: %s: %v", domain.ErrDOMChanged, name, err)
	}
	return value, nil
}

// inputValue reads the current value of a form field, which is what the broker
// will actually submit — not what Goroker typed.
func (p *pageCtx) inputValue(ctx context.Context, name, selector string) (string, error) {
	el, err := p.element(ctx, name, selector)
	if err != nil {
		return "", err
	}
	prop, err := el.Property("value")
	if err != nil {
		return "", fmt.Errorf("%w: read value of %s: %v", domain.ErrDOMChanged, name, err)
	}
	return strings.TrimSpace(prop.String()), nil
}

// fill clears a form field and types value into it. The value is not verified
// here: callers re-read the whole order from the DOM afterwards.
func (p *pageCtx) fill(ctx context.Context, name, selector, value string) error {
	el, err := p.element(ctx, name, selector)
	if err != nil {
		return err
	}
	if err := el.Focus(); err != nil {
		return fmt.Errorf("%w: focus %s: %v", domain.ErrDOMChanged, name, err)
	}
	if err := el.SelectAllText(); err != nil {
		return fmt.Errorf("%w: select text of %s: %v", domain.ErrDOMChanged, name, err)
	}
	if err := el.Input(value); err != nil {
		return fmt.Errorf("%w: type into %s: %v", domain.ErrDOMChanged, name, err)
	}
	return nil
}

// click clicks a required element.
func (p *pageCtx) click(ctx context.Context, name, selector string) error {
	el, err := p.element(ctx, name, selector)
	if err != nil {
		return err
	}
	if err := el.Click("left", 1); err != nil {
		return fmt.Errorf("%w: click %s: %v", domain.ErrDOMChanged, name, err)
	}
	return nil
}

// present reports whether an optional selector currently matches something.
func (p *pageCtx) present(ctx context.Context, selector string, wait time.Duration) (bool, error) {
	_, ok, err := p.optionalElement(ctx, selector, wait)
	return ok, err
}

// challenge reports whether a security challenge (CAPTCHA, OTP, SMS, device
// confirmation) is on screen. Goroker never tries to solve one: it hands the
// browser back to the user and waits.
func (p *pageCtx) challenge(ctx context.Context) (string, bool, error) {
	for _, marker := range p.sel.ChallengeMarkers {
		ok, err := p.present(ctx, marker, 500*time.Millisecond)
		if err != nil {
			return "", false, err
		}
		if ok {
			return marker, true, nil
		}
	}
	return "", false, nil
}

// assertNoUnexpectedModal aborts when a dialog that is not part of a known
// flow is covering the page.
func (p *pageCtx) assertNoUnexpectedModal(ctx context.Context) error {
	ok, err := p.present(ctx, p.sel.UnexpectedModal, 300*time.Millisecond)
	if err != nil {
		return err
	}
	if ok {
		return fmt.Errorf("%w: a dialog is open on the broker page", domain.ErrUnexpectedModal)
	}
	return nil
}

// matchAny reports whether text equals one of the candidate labels after digit
// and letter normalization. Comparison is exact per candidate; there is no
// substring guessing for market state.
func matchAny(text string, candidates []string) bool {
	normalized := broker.NormalizeSymbol(broker.NormalizeDigits(text))
	for _, c := range candidates {
		if broker.NormalizeSymbol(broker.NormalizeDigits(c)) == normalized {
			return true
		}
	}
	return false
}

// containsAny reports whether text contains one of the candidate labels. It is
// used only for broker result messages, which embed their status in a sentence.
func containsAny(text string, candidates []string) bool {
	normalized := broker.NormalizeSymbol(broker.NormalizeDigits(text))
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if strings.Contains(normalized, broker.NormalizeSymbol(broker.NormalizeDigits(c))) {
			return true
		}
	}
	return false
}
