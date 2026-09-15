// Package iranbroker is the adapter for the user's Iranian brokerage web UI.
//
// Every brokerage-specific DOM selector in Goroker lives in this file. Nothing
// above this package may contain a selector.
//
// The selector values are empty until they have been established by inspecting
// the real brokerage site (see docs/broker-ui-analysis.md). They are never
// guessed: an empty selector makes the corresponding operation fail closed with
// domain.ErrSelectorMissing rather than act on the wrong element.
package iranbroker

import (
	"fmt"
	"os"
	"reflect"
	"strings"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"

	"github.com/30nap/goroker/internal/domain"
)

// Selectors holds every CSS selector the adapter uses.
//
// Selector strategy, in order of preference:
//  1. stable element IDs
//  2. data-* attributes
//  3. accessibility attributes (role, aria-label)
//  4. labels and associated form controls
//  5. stable semantic selectors
//
// Positional selectors such as `div:nth-child(6) > div:nth-child(3)` are a last
// resort; any selector of that kind must be recorded in Fragile below and
// documented in docs/broker-ui-analysis.md.
type Selectors struct {
	// --- authentication ---------------------------------------------------
	// LoggedInMarker is an element that exists only on an authenticated page.
	LoggedInMarker string `koanf:"logged_in_marker" yaml:"logged_in_marker"`
	// LoginForm, UsernameInput, PasswordInput and LoginButton drive the login
	// form. They are used only when credentials are configured locally.
	LoginForm     string `koanf:"login_form" yaml:"login_form"`
	UsernameInput string `koanf:"username_input" yaml:"username_input"`
	PasswordInput string `koanf:"password_input" yaml:"password_input"`
	LoginButton   string `koanf:"login_button" yaml:"login_button"`
	// ChallengeMarkers are elements that indicate a CAPTCHA, OTP, SMS, device
	// confirmation or other security challenge. Their presence pauses
	// automation so the user can complete the challenge manually.
	ChallengeMarkers []string `koanf:"challenge_markers" yaml:"challenge_markers"`
	// LoginError is the error message shown for a rejected login.
	LoginError string `koanf:"login_error" yaml:"login_error"`

	// --- market status ----------------------------------------------------
	// MarketStatusIndicator is the element whose text states whether the
	// market is open.
	MarketStatusIndicator string `koanf:"market_status_indicator" yaml:"market_status_indicator"`
	// MarketOpenText and MarketClosedText are the exact texts that positively
	// mean open and closed. Any other text yields MarketUnknown.
	MarketOpenText   []string `koanf:"market_open_text" yaml:"market_open_text"`
	MarketClosedText []string `koanf:"market_closed_text" yaml:"market_closed_text"`

	// --- symbol and quote -------------------------------------------------
	SymbolSearch      string `koanf:"symbol_search" yaml:"symbol_search"`
	SymbolResult      string `koanf:"symbol_result" yaml:"symbol_result"`
	SymbolResultName  string `koanf:"symbol_result_name" yaml:"symbol_result_name"`
	SymbolResultTitle string `koanf:"symbol_result_title" yaml:"symbol_result_title"`
	SelectedSymbol    string `koanf:"selected_symbol" yaml:"selected_symbol"`
	LastPrice         string `koanf:"last_price" yaml:"last_price"`
	BestAsk           string `koanf:"best_ask" yaml:"best_ask"`
	BestBid           string `koanf:"best_bid" yaml:"best_bid"`
	// QuoteUpdatedAt is an element carrying the broker's own update time, when
	// the UI exposes one. Freshness is measured locally regardless.
	QuoteUpdatedAt string `koanf:"quote_updated_at" yaml:"quote_updated_at"`

	// --- order form -------------------------------------------------------
	BuyTab        string `koanf:"buy_tab" yaml:"buy_tab"`
	OrderSideMark string `koanf:"order_side_mark" yaml:"order_side_mark"`
	PriceInput    string `koanf:"price_input" yaml:"price_input"`
	QuantityInput string `koanf:"quantity_input" yaml:"quantity_input"`
	// OrderSymbolField is where the order form shows the instrument it will
	// trade, so the symbol can be read back from the DOM.
	OrderSymbolField string `koanf:"order_symbol_field" yaml:"order_symbol_field"`
	// EstimatedCost is the broker's own order value estimate, when shown.
	EstimatedCost string `koanf:"estimated_cost" yaml:"estimated_cost"`
	// PriceLimitMin/Max are the allowed daily price band, when shown.
	PriceLimitMin string `koanf:"price_limit_min" yaml:"price_limit_min"`
	PriceLimitMax string `koanf:"price_limit_max" yaml:"price_limit_max"`
	SubmitButton  string `koanf:"submit_button" yaml:"submit_button"`
	// SubmitConfirmDialog is the broker's own confirmation dialog, if it has
	// one, with the element that accepts it.
	SubmitConfirmDialog string `koanf:"submit_confirm_dialog" yaml:"submit_confirm_dialog"`
	SubmitConfirmAccept string `koanf:"submit_confirm_accept" yaml:"submit_confirm_accept"`
	// ResultMessage, OrderID and result texts describe the broker's response.
	ResultMessage      string   `koanf:"result_message" yaml:"result_message"`
	ResultOrderID      string   `koanf:"result_order_id" yaml:"result_order_id"`
	ResultAcceptedText []string `koanf:"result_accepted_text" yaml:"result_accepted_text"`
	ResultRejectedText []string `koanf:"result_rejected_text" yaml:"result_rejected_text"`

	// --- interruptions ----------------------------------------------------
	// UnexpectedModal matches modal dialogs that are not part of a known flow.
	// Their presence aborts whatever was in progress.
	UnexpectedModal string `koanf:"unexpected_modal" yaml:"unexpected_modal"`

	// Fragile lists the field names whose selectors are positional or
	// otherwise brittle, so they can be reported and reviewed after a UI change.
	Fragile []string `koanf:"fragile" yaml:"fragile"`
}

// SelectorsFileName is the optional per-user override file inside ~/.goroker.
// It lets a broker UI change be fixed without rebuilding, and keeps the
// user's own findings out of the repository.
const SelectorsFileName = "selectors.yaml"

// Default returns the compiled-in selector set.
//
// It is intentionally empty: the selectors for the user's brokerage are
// established during broker discovery (Phase 2) by inspecting the real site,
// and are then filled in here or supplied through ~/.goroker/selectors.yaml.
func Default() Selectors {
	return Selectors{}
}

// Load returns the compiled-in selectors overlaid with path, when that file
// exists. A missing file is not an error.
func Load(path string) (Selectors, error) {
	sel := Default()
	if path == "" {
		return sel, nil
	}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return sel, nil
		}
		return sel, fmt.Errorf("stat %s: %w", path, err)
	}
	k := koanf.New(".")
	if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
		return sel, fmt.Errorf("read %s: %w", path, err)
	}
	if err := k.UnmarshalWithConf("", &sel, koanf.UnmarshalConf{Tag: "koanf"}); err != nil {
		return sel, fmt.Errorf("decode %s: %w", path, err)
	}
	return sel, nil
}

// Require returns the named selector, or ErrSelectorMissing when it is empty.
// Every DOM access goes through this, so an unconfigured selector can never
// silently match the wrong element.
func Require(name, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%w: %s is not configured; run broker discovery and fill it in %s",
			domain.ErrSelectorMissing, name, SelectorsFileName)
	}
	return value, nil
}

// Missing lists the selector fields that are still empty. `goroker status`
// reports it so the user knows what discovery is outstanding.
func (s Selectors) Missing() []string {
	var missing []string
	v := reflect.ValueOf(s)
	t := v.Type()
	for i := range t.NumField() {
		field := t.Field(i)
		if field.Name == "Fragile" {
			continue
		}
		switch field.Type.Kind() {
		case reflect.String:
			if strings.TrimSpace(v.Field(i).String()) == "" {
				missing = append(missing, field.Tag.Get("koanf"))
			}
		case reflect.Slice:
			if v.Field(i).Len() == 0 {
				missing = append(missing, field.Tag.Get("koanf"))
			}
		}
	}
	return missing
}

// Configured reports whether every selector needed for trading is present.
func (s Selectors) Configured() bool { return len(s.Missing()) == 0 }
