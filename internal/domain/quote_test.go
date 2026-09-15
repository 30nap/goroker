package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/30nap/goroker/internal/domain"
)

func TestQuoteFreshness(t *testing.T) {
	now := time.Date(2026, 9, 15, 10, 42, 31, 0, time.UTC)
	maxAge := 5 * time.Second

	cases := []struct {
		name      string
		timestamp time.Time
		want      bool
	}{
		{"just observed", now, true},
		{"within limit", now.Add(-4 * time.Second), true},
		{"exactly at limit", now.Add(-5 * time.Second), true},
		{"older than limit", now.Add(-6 * time.Second), false},
		{"no timestamp", time.Time{}, false},
		{"timestamped in the future", now.Add(time.Second), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q := domain.Quote{Symbol: "فولاد", BestAsk: 3310, Timestamp: tc.timestamp}
			if got := q.IsFresh(now, maxAge); got != tc.want {
				t.Fatalf("IsFresh() = %v, want %v", got, tc.want)
			}
		})
	}

	q := domain.Quote{Symbol: "فولاد", BestAsk: 3310, Timestamp: now}
	if q.IsFresh(now, 0) {
		t.Fatal("IsFresh() with no configured max age must be false")
	}
}

func TestQuoteValidate(t *testing.T) {
	now := time.Now()
	valid := domain.Quote{Symbol: "فولاد", LastPrice: 3310, BestAsk: 3310, BestBid: 3305, Timestamp: now}
	if err := valid.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}

	for _, q := range []domain.Quote{
		{BestAsk: 3310, Timestamp: now},                // no symbol
		{Symbol: "فولاد", BestAsk: 3310},               // no timestamp
		{Symbol: "فولاد", BestAsk: -1, Timestamp: now}, // negative price
	} {
		if err := q.Validate(); !errors.Is(err, domain.ErrQuoteInvalid) {
			t.Fatalf("Validate(%+v) = %v, want ErrQuoteInvalid", q, err)
		}
	}
}

func TestQuotePriceSource(t *testing.T) {
	q := domain.Quote{Symbol: "فولاد", LastPrice: 3300, BestAsk: 3310, BestBid: 3305, Timestamp: time.Now()}

	ask, err := q.Price(domain.PriceSourceBestAsk)
	if err != nil {
		t.Fatalf("Price(BEST_ASK) = %v", err)
	}
	if ask != 3310 {
		t.Fatalf("Price(BEST_ASK) = %d, want 3310", ask)
	}

	// An unknown source must not silently fall back to another price field.
	if _, err := q.Price(domain.PriceSource("MID")); !errors.Is(err, domain.ErrQuoteInvalid) {
		t.Fatalf("Price(MID) = %v, want ErrQuoteInvalid", err)
	}
}
