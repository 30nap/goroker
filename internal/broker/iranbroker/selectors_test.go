package iranbroker_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/30nap/goroker/internal/broker/iranbroker"
	"github.com/30nap/goroker/internal/domain"
)

// TestUnconfiguredSelectorFailsClosed proves that a selector which broker
// discovery has not established yet cannot be used: it aborts instead of
// matching some other element.
func TestUnconfiguredSelectorFailsClosed(t *testing.T) {
	for _, value := range []string{"", "   ", "\t\n"} {
		if _, err := iranbroker.Require("price_input", value); !errors.Is(err, domain.ErrSelectorMissing) {
			t.Fatalf("Require(%q) = %v, want ErrSelectorMissing", value, err)
		}
	}

	got, err := iranbroker.Require("price_input", "#order-price")
	if err != nil {
		t.Fatalf("Require() = %v, want nil", err)
	}
	if got != "#order-price" {
		t.Fatalf("Require() = %q, want the selector back unchanged", got)
	}
}

// TestDefaultSelectorsAreEmpty guards the rule that selectors are never
// invented: the compiled-in set stays empty until the real brokerage UI has
// been inspected.
func TestDefaultSelectorsAreEmpty(t *testing.T) {
	sel := iranbroker.Default()
	if sel.Configured() {
		t.Fatal("the default selector set claims to be configured")
	}
	missing := sel.Missing()
	for _, want := range []string{"price_input", "quantity_input", "submit_button", "best_ask", "market_status_indicator"} {
		if !slices.Contains(missing, want) {
			t.Errorf("Missing() does not report %q", want)
		}
	}
}

// TestLoadOverrides proves a user-supplied selectors.yaml is picked up, so a
// broker UI change can be fixed without rebuilding.
func TestLoadOverrides(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, iranbroker.SelectorsFileName)
	content := `
best_ask: "#best-ask"
price_input: "#order-price"
market_open_text:
  - "بازار باز است"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write selectors: %v", err)
	}

	sel, err := iranbroker.Load(path)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if sel.BestAsk != "#best-ask" || sel.PriceInput != "#order-price" {
		t.Fatalf("Load() = %+v, want the file values", sel)
	}
	if len(sel.MarketOpenText) != 1 {
		t.Fatalf("market_open_text = %v, want one entry", sel.MarketOpenText)
	}
	// Anything the file does not mention stays unset, and therefore unusable.
	if sel.SubmitButton != "" {
		t.Fatal("an unmentioned selector was filled in from somewhere")
	}
	if sel.Configured() {
		t.Fatal("a partially filled selector set claims to be configured")
	}
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	sel, err := iranbroker.Load(filepath.Join(t.TempDir(), "does-not-exist.yaml"))
	if err != nil {
		t.Fatalf("Load() = %v, want nil for a missing file", err)
	}
	if sel.Configured() {
		t.Fatal("a missing file produced a configured selector set")
	}
}
