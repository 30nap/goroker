package inspect_test

import (
	"strings"
	"testing"

	"github.com/30nap/goroker/internal/inspect"
)

func TestSanitizeKeepsWhatSelectorsNeed(t *testing.T) {
	raw := `
<div id="order-panel" class="MuiPaper-root jss1234567890-container" data-testid="buy-form" aria-label="فرم خرید">
  <label for="order-price">قیمت</label>
  <input id="order-price" class="price-input" name="price" type="text" placeholder="قیمت" value="3310">
  <span class="best-ask">3,310</span>
  <button type="submit" class="submit-btn" data-action="send-buy">ارسال خرید</button>
</div>`

	out, err := inspect.Sanitize(raw)
	if err != nil {
		t.Fatalf("Sanitize() = %v", err)
	}

	for _, want := range []string{
		`id="order-price"`,
		`class="price-input"`,
		`data-testid="buy-form"`,
		`data-action="send-buy"`,
		`aria-label="فرم خرید"`,
		`placeholder="قیمت"`,
		`jss1234567890-container`, // a class that looks like a token must survive
		"ارسال خرید",
		"3,310", // prices are needed to identify the quote elements
	} {
		if !strings.Contains(out, want) {
			t.Errorf("sanitized output lost %q:\n%s", want, out)
		}
	}

	// The value the user typed is not part of the page structure.
	if strings.Contains(out, `value="3310"`) {
		t.Errorf("sanitized output kept a form value:\n%s", out)
	}
}

func TestSanitizeRemovesSensitiveContent(t *testing.T) {
	raw := `
<div class="account">
  <script>var token = "abcdef";</script>
  <style>.a{color:red}</style>
  <span class="owner">سینا</span>
  <span class="account-number">6104337812345678</span>
  <span class="national-id">0012345678</span>
  <span class="phone">09123456789</span>
  <span class="email">someone@example.com</span>
  <input type="password" name="password" value="hunter2">
  <input type="text" name="otp" value="489210">
  <a href="/orders?session=eyJhbGciOiJIUzI1NiJ9.body.sig">سفارش‌ها</a>
  <img src="data:image/png;base64,iVBORw0KGgoAAAANSUhEUg">
  <div onclick="submitOrder()">x</div>
</div>`

	out, err := inspect.Sanitize(raw)
	if err != nil {
		t.Fatalf("Sanitize() = %v", err)
	}

	for _, secret := range []string{
		"hunter2",
		"489210",
		"6104337812345678",
		"0012345678",
		"09123456789",
		"someone@example.com",
		"eyJhbGciOiJIUzI1NiJ9",
		"iVBORw0KGgoAAAANSUhEUg",
		`var token`,
		"onclick",
	} {
		if strings.Contains(out, secret) {
			t.Errorf("sanitized output still contains %q:\n%s", secret, out)
		}
	}

	// Structure around the removed data survives, so selectors stay writable.
	for _, want := range []string{`class="account-number"`, `name="otp"`, `type="password"`, `href="/orders?`} {
		if !strings.Contains(out, want) {
			t.Errorf("sanitized output lost structure %q:\n%s", want, out)
		}
	}
}

func TestRedactTextKeepsPrices(t *testing.T) {
	cases := map[string]bool{ // text -> should survive unchanged
		"3310":          true,
		"3,310":         true,
		"33,100,000":    true,
		"۳,۳۱۰":         true,
		"بازار باز است": true,
		"10,000":        true,
	}
	for text, keep := range cases {
		got := inspect.RedactText(text)
		if keep && got != text {
			t.Errorf("RedactText(%q) = %q, want it unchanged", text, got)
		}
	}

	if got := inspect.RedactText("شماره حساب 6104337812345678"); strings.Contains(got, "6104337812345678") {
		t.Errorf("RedactText kept an account number: %q", got)
	}
}
