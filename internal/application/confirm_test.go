package application_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/30nap/goroker/internal/application"
	"github.com/30nap/goroker/internal/domain"
)

func samplePrompt(timeout time.Duration) application.ConfirmationPrompt {
	return application.ConfirmationPrompt{
		Order: domain.PreparedOrder{
			Symbol:      "فولاد",
			Side:        domain.SideBuy,
			Price:       3310,
			Quantity:    10000,
			PriceSource: domain.PriceSourceBestAsk,
		},
		CurrentBestAsk: 3310,
		MinPrice:       3250,
		MaxPrice:       3350,
		QuoteAge:       time.Second,
		Timeout:        timeout,
	}
}

// TestOnlyExactWordConfirms proves that nothing except the exact word BUY
// approves an order.
func TestOnlyExactWordConfirms(t *testing.T) {
	cases := []struct {
		input string
		want  error
	}{
		{"BUY\n", nil},
		{"BUY\r\n", nil},
		{"  BUY  \n", nil}, // terminals add whitespace; the word itself is exact
		{"buy\n", domain.ErrNotConfirmed},
		{"Buy\n", domain.ErrNotConfirmed},
		{"BUY!\n", domain.ErrNotConfirmed},
		{"BUY 10000\n", domain.ErrNotConfirmed},
		{"y\n", domain.ErrNotConfirmed},
		{"yes\n", domain.ErrNotConfirmed},
		{"\n", domain.ErrNotConfirmed},
		{"", domain.ErrNotConfirmed}, // closed stdin, e.g. non-interactive use
		{"خرید\n", domain.ErrNotConfirmed},
	}

	for _, tc := range cases {
		name := strings.TrimSpace(tc.input)
		if name == "" {
			name = "empty"
		}
		t.Run(name, func(t *testing.T) {
			var out bytes.Buffer
			confirmer := application.NewTerminalConfirmer(strings.NewReader(tc.input), &out)

			err := confirmer.Confirm(context.Background(), samplePrompt(time.Second))
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Confirm(%q) = %v, want nil", tc.input, err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Confirm(%q) = %v, want %v", tc.input, err, tc.want)
			}
		})
	}
}

// TestConfirmationExpires proves the prompt stops accepting input after the
// timeout instead of waiting forever.
func TestConfirmationExpires(t *testing.T) {
	var out bytes.Buffer
	// A reader that never produces a line stands in for a user who walks away.
	confirmer := application.NewTerminalConfirmer(blockingReader{}, &out)

	start := time.Now()
	err := confirmer.Confirm(context.Background(), samplePrompt(50*time.Millisecond))
	if !errors.Is(err, domain.ErrConfirmationExpire) {
		t.Fatalf("Confirm() = %v, want ErrConfirmationExpire", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("Confirm() took %s, want it to expire promptly", elapsed)
	}
	if !strings.Contains(out.String(), "expired") {
		t.Fatalf("output %q does not tell the user the confirmation expired", out.String())
	}
}

// TestConfirmationCancelled covers Ctrl+C at the prompt.
func TestConfirmationCancelled(t *testing.T) {
	var out bytes.Buffer
	confirmer := application.NewTerminalConfirmer(blockingReader{}, &out)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := confirmer.Confirm(ctx, samplePrompt(time.Minute)); !errors.Is(err, domain.ErrCancelled) {
		t.Fatalf("Confirm() = %v, want ErrCancelled", err)
	}
}

// TestConfirmationPromptShowsTheOrder makes sure the user sees what they are
// approving, including the price source and the allowed range.
func TestConfirmationPromptShowsTheOrder(t *testing.T) {
	text := application.RenderConfirmation(samplePrompt(15 * time.Second))
	for _, want := range []string{
		"GOROKER ORDER CONFIRMATION",
		"فولاد",
		"BUY",
		"10,000",
		"3,310",
		"33,100,000",
		"BEST_ASK",
		"3,250 .. 3,350",
		"Type BUY to submit",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("confirmation screen is missing %q:\n%s", want, text)
		}
	}
}

func TestThousands(t *testing.T) {
	cases := map[int64]string{
		0:        "0",
		7:        "7",
		999:      "999",
		1000:     "1,000",
		10000:    "10,000",
		33100000: "33,100,000",
		-1234567: "-1,234,567",
	}
	for in, want := range cases {
		if got := application.Thousands(in); got != want {
			t.Errorf("Thousands(%d) = %q, want %q", in, got, want)
		}
	}
}

// blockingReader never returns, standing in for an idle terminal.
type blockingReader struct{}

func (blockingReader) Read([]byte) (int, error) {
	select {} //nolint:staticcheck // blocks for the lifetime of the test
}
