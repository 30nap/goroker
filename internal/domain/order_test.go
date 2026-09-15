package domain_test

import (
	"errors"
	"testing"
	"time"

	"github.com/30nap/goroker/internal/domain"
)

func TestBuyOrderRequestValidate(t *testing.T) {
	cases := []struct {
		name string
		req  domain.BuyOrderRequest
		want error
	}{
		{
			name: "range request is valid",
			req:  domain.BuyOrderRequest{Symbol: "فولاد", MinPrice: 3250, MaxPrice: 3350, Quantity: 10000},
		},
		{
			name: "exact price request is valid",
			req:  domain.BuyOrderRequest{Symbol: "فولاد", Price: 3310, Quantity: 10000},
		},
		{
			name: "empty symbol aborts",
			req:  domain.BuyOrderRequest{MinPrice: 1, MaxPrice: 2, Quantity: 1},
			want: domain.ErrAbort,
		},
		{
			name: "zero quantity aborts",
			req:  domain.BuyOrderRequest{Symbol: "فولاد", MinPrice: 1, MaxPrice: 2},
			want: domain.ErrAbort,
		},
		{
			name: "negative quantity aborts",
			req:  domain.BuyOrderRequest{Symbol: "فولاد", MinPrice: 1, MaxPrice: 2, Quantity: -5},
			want: domain.ErrAbort,
		},
		{
			name: "inverted range aborts",
			req:  domain.BuyOrderRequest{Symbol: "فولاد", MinPrice: 3350, MaxPrice: 3250, Quantity: 10},
			want: domain.ErrAbort,
		},
		{
			name: "missing range aborts",
			req:  domain.BuyOrderRequest{Symbol: "فولاد", Quantity: 10},
			want: domain.ErrAbort,
		},
		{
			name: "exact price combined with a range aborts",
			req:  domain.BuyOrderRequest{Symbol: "فولاد", Price: 3310, MinPrice: 3250, MaxPrice: 3350, Quantity: 10},
			want: domain.ErrAbort,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Validate() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want error wrapping %v", err, tc.want)
			}
		})
	}
}

func TestBuyOrderRequestRange(t *testing.T) {
	rangeReq := domain.BuyOrderRequest{Symbol: "فولاد", MinPrice: 3250, MaxPrice: 3350, Quantity: 10}
	cases := []struct {
		price int64
		want  bool
	}{
		{3249, false}, // below range
		{3250, true},  // lower bound is inclusive
		{3300, true},  // inside range
		{3350, true},  // upper bound is inclusive
		{3351, false}, // above range
	}
	for _, tc := range cases {
		if got := rangeReq.InRange(tc.price); got != tc.want {
			t.Errorf("InRange(%d) = %v, want %v", tc.price, got, tc.want)
		}
	}

	exact := domain.BuyOrderRequest{Symbol: "فولاد", Price: 3310, Quantity: 10}
	if min, max := exact.Bounds(); min != 3310 || max != 3310 {
		t.Fatalf("exact bounds = %d..%d, want 3310..3310", min, max)
	}
	if exact.InRange(3309) || exact.InRange(3311) {
		t.Fatal("exact-price request accepted a different price")
	}
	if !exact.InRange(3310) {
		t.Fatal("exact-price request rejected its own price")
	}
}

func TestPreparedOrderMatches(t *testing.T) {
	expected := domain.PreparedOrder{
		Symbol:   "فولاد",
		Side:     domain.SideBuy,
		Price:    3310,
		Quantity: 10000,
	}

	cases := []struct {
		name string
		dom  domain.PreparedOrder
		want error
	}{
		{
			name: "identical order matches",
			dom:  expected,
		},
		{
			name: "wrong symbol aborts",
			dom:  domain.PreparedOrder{Symbol: "خودرو", Side: domain.SideBuy, Price: 3310, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "wrong side aborts",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideSell, Price: 3310, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "empty side aborts",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Price: 3310, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "wrong price aborts",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideBuy, Price: 3311, Quantity: 10000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "wrong quantity aborts",
			dom:  domain.PreparedOrder{Symbol: "فولاد", Side: domain.SideBuy, Price: 3310, Quantity: 1000},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "inconsistent estimated cost aborts",
			dom: domain.PreparedOrder{
				Symbol: "فولاد", Side: domain.SideBuy, Price: 3310, Quantity: 10000,
				EstimatedCost: 1,
			},
			want: domain.ErrOrderMismatch,
		},
		{
			name: "consistent estimated cost matches",
			dom: domain.PreparedOrder{
				Symbol: "فولاد", Side: domain.SideBuy, Price: 3310, Quantity: 10000,
				EstimatedCost: 33_100_000,
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := expected.Matches(tc.dom)
			if tc.want == nil {
				if err != nil {
					t.Fatalf("Matches() = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("Matches() = %v, want error wrapping %v", err, tc.want)
			}
		})
	}
}

func TestPreparedOrderExpectedCost(t *testing.T) {
	order := domain.PreparedOrder{Price: 3310, Quantity: 10000, PreparedAt: time.Now()}
	if got := order.ExpectedCost(); got != 33_100_000 {
		t.Fatalf("ExpectedCost() = %d, want 33100000", got)
	}
}
