package broker

import (
	"fmt"
	"strings"

	"github.com/30nap/goroker/internal/domain"
)

// ParsePrice converts a price or quantity as displayed by an Iranian broker UI
// into an integer. It accepts Persian (۰-۹) and Arabic-Indic (٠-٩) digits,
// thousands separators (",", "٬", "、", ".", U+066C, and the Persian/Arabic
// space variants), and surrounding currency words.
//
// It deliberately refuses anything it cannot read exactly — including
// fractional values — because a misread price is an order at the wrong price.
func ParsePrice(raw string) (int64, error) {
	normalized := NormalizeDigits(raw)

	var b strings.Builder
	seenDigit := false
	for _, r := range normalized {
		switch {
		case r >= '0' && r <= '9':
			seenDigit = true
			b.WriteRune(r)
		case r == ',' || r == '.' || r == '٬' || r == '٫' || r == '\'' || r == '_':
			// Separator: skipped. A decimal point is treated as a separator
			// only when what follows is a full thousands group; anything else
			// is rejected below by the trailing-digit check.
		case r == ' ' || r == '\t' || r == '\n' || r == '\r' ||
			r == ' ' || r == '‌' || r == '‏' || r == '‎':
			// Whitespace and bidi marks are ignored.
		default:
			if seenDigit {
				// Trailing text such as "ریال" ends the number.
				return finishPrice(raw, normalized, b.String())
			}
			// Leading text such as "قیمت:" is skipped.
		}
	}
	return finishPrice(raw, normalized, b.String())
}

func finishPrice(raw, normalized, digits string) (int64, error) {
	if digits == "" {
		return 0, fmt.Errorf("%w: no digits in %q", domain.ErrQuoteInvalid, raw)
	}
	if err := rejectFraction(normalized); err != nil {
		return 0, err
	}
	var value int64
	for _, r := range digits {
		d := int64(r - '0')
		if value > (1<<62)/10 {
			return 0, fmt.Errorf("%w: value out of range in %q", domain.ErrQuoteInvalid, raw)
		}
		value = value*10 + d
	}
	return value, nil
}

// rejectFraction refuses values with a genuine decimal part. Separators in
// Iranian price displays group thousands (three digits); a group of one or two
// digits after a dot is a fraction, and Goroker does not guess how to round it.
func rejectFraction(s string) error {
	for i, r := range s {
		if r != '.' && r != '٫' {
			continue
		}
		rest := s[i+len(string(r)):]
		digits := 0
		for _, c := range rest {
			if c >= '0' && c <= '9' {
				digits++
				continue
			}
			break
		}
		if digits > 0 && digits != 3 {
			return fmt.Errorf("%w: fractional value %q is not an exact integer price", domain.ErrQuoteInvalid, s)
		}
	}
	return nil
}

// NormalizeDigits rewrites Persian and Arabic-Indic digits as ASCII digits and
// normalizes the Arabic yeh/kaf variants that Iranian sites mix freely, so that
// symbol comparison and number parsing are stable.
func NormalizeDigits(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r >= '۰' && r <= '۹': // U+06F0..U+06F9 Extended Arabic-Indic
			b.WriteRune('0' + (r - '۰'))
		case r >= '٠' && r <= '٩': // U+0660..U+0669 Arabic-Indic
			b.WriteRune('0' + (r - '٠'))
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizeSymbol prepares a symbol for exact comparison: Arabic yeh/kaf are
// mapped to their Persian forms, zero-width non-joiners and bidi marks are
// dropped, and surrounding whitespace is trimmed.
//
// This is normalization, not fuzzy matching: two symbols are equal only if
// they are the same sequence of letters.
func NormalizeSymbol(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case 'ي': // U+064A Arabic yeh
			b.WriteRune('ی') // U+06CC Farsi yeh
		case 'ك': // U+0643 Arabic kaf
			b.WriteRune('ک') // U+06A9 Keheh
		case 'ۀ':
			b.WriteRune('ه')
		case '\u200C', '\u200D', '\u200E', '\u200F', '\u00A0', '\uFEFF':
			// Zero-width and bidi control characters carry no identity.
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// SymbolsEqual reports whether two symbol strings denote the same instrument.
// Matching is exact after normalization; no prefix or substring matching.
func SymbolsEqual(a, b string) bool {
	return NormalizeSymbol(a) == NormalizeSymbol(b)
}
