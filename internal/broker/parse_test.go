package broker_test

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/30nap/goroker/internal/broker"
	"github.com/30nap/goroker/internal/domain"
)

type priceSamples struct {
	Valid []struct {
		Raw  string `json:"raw"`
		Want int64  `json:"want"`
	} `json:"valid"`
	Invalid []struct {
		Raw string `json:"raw"`
		Why string `json:"why"`
	} `json:"invalid"`
}

func loadSamples(t *testing.T) priceSamples {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "price-samples.json"))
	if err != nil {
		t.Fatalf("read fixtures: %v", err)
	}
	var samples priceSamples
	if err := json.Unmarshal(data, &samples); err != nil {
		t.Fatalf("decode fixtures: %v", err)
	}
	return samples
}

func TestParsePrice(t *testing.T) {
	samples := loadSamples(t)

	for _, tc := range samples.Valid {
		got, err := broker.ParsePrice(tc.Raw)
		if err != nil {
			t.Errorf("ParsePrice(%q) = %v, want %d", tc.Raw, err, tc.Want)
			continue
		}
		if got != tc.Want {
			t.Errorf("ParsePrice(%q) = %d, want %d", tc.Raw, got, tc.Want)
		}
	}

	for _, tc := range samples.Invalid {
		if got, err := broker.ParsePrice(tc.Raw); !errors.Is(err, domain.ErrQuoteInvalid) {
			t.Errorf("ParsePrice(%q) = (%d, %v), want ErrQuoteInvalid (%s)", tc.Raw, got, err, tc.Why)
		}
	}
}

func TestSymbolsEqual(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"فولاد", "فولاد", true},
		{"فولاد", " فولاد ", true},
		{"فولاد", "فولاد‌", true}, // zero-width non-joiner
		{"کیمیا", "كيميا", true},  // Arabic kaf/yeh written as Persian
		{"فولاد", "فولاژ", false},
		{"فولاد", "فولا", false}, // no prefix matching
		{"فولاد", "ذوب", false},
		{"وبملت", "وبملت1", false},
	}
	for _, tc := range cases {
		if got := broker.SymbolsEqual(tc.a, tc.b); got != tc.want {
			t.Errorf("SymbolsEqual(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestNormalizeDigits(t *testing.T) {
	if got := broker.NormalizeDigits("۱۲۳٤٥"); got != "12345" {
		t.Fatalf("NormalizeDigits() = %q, want \"12345\"", got)
	}
}
