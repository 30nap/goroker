// Package inspect supports broker discovery: it captures the structure of a
// brokerage page so selectors can be established from what is really there.
//
// Captured HTML is sanitised before it is written to disk. Discovery snapshots
// are meant to be read and shared while working on the adapter, and a brokerage
// page shows account data, so scripts, form values, tokens and personal
// identifiers are stripped. Sanitising is best effort: read a snapshot before
// sharing it.
package inspect

import (
	"fmt"
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"

	"github.com/30nap/goroker/internal/logging"
)

// Redacted replaces a removed value.
const Redacted = "[REDACTED]"

// droppedContent replaces the body of an element whose content is never useful
// for selector work.
const droppedContent = "/* removed by goroker inspect */"

// contentDropped are elements whose children are removed entirely: they carry
// no selector information and can be very large or contain session data.
var contentDropped = map[string]bool{
	"script":   true,
	"style":    true,
	"noscript": true,
	"svg":      true,
	"canvas":   true,
	"template": true,
}

// valueBearing are elements whose current value is user data.
var valueBearing = map[string]bool{
	"input":    true,
	"textarea": true,
	"select":   true,
	"option":   true,
}

var (
	// digitRun matches any number as written on the page, separators included.
	// redactIdentifiers decides which of these runs are identifiers.
	digitRun = regexp.MustCompile(`[\d۰-۹٠-٩][\d۰-۹٠-٩,٬']*`)
	// iranPhone matches a local mobile number.
	iranPhone = regexp.MustCompile(`0?9\d{9}`)
	// email matches an address.
	email = regexp.MustCompile(`[\w.+-]+@[\w-]+\.[\w.-]+`)
	// tokenish matches a long opaque string, which is what session tokens,
	// JWTs and signed URLs look like.
	tokenish = regexp.MustCompile(`[A-Za-z0-9_\-]{28,}\.?[A-Za-z0-9_\-.]*`)
)

// Sanitize parses raw HTML, strips everything that is not needed to write a
// selector, and returns the result.
func Sanitize(raw string) (string, error) {
	nodes, err := html.ParseFragment(strings.NewReader(raw), &html.Node{
		Type:     html.ElementNode,
		Data:     "body",
		DataAtom: atom.Body,
	})
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}

	var out strings.Builder
	for _, n := range nodes {
		clean(n)
		if err := html.Render(&out, n); err != nil {
			return "", fmt.Errorf("render html: %w", err)
		}
	}
	return out.String(), nil
}

func clean(n *html.Node) {
	if n.Type == html.CommentNode {
		n.Data = droppedContent
		return
	}
	if n.Type == html.TextNode {
		n.Data = RedactText(n.Data)
		return
	}
	if n.Type == html.ElementNode {
		tag := strings.ToLower(n.Data)
		n.Attr = cleanAttrs(tag, n.Attr)

		if contentDropped[tag] {
			removeChildren(n)
			n.AppendChild(&html.Node{Type: html.TextNode, Data: droppedContent})
			return
		}
		if tag == "option" {
			// Option labels name instruments and order types, which is useful,
			// but they are still page text and get the same redaction.
		}
	}

	for child := n.FirstChild; child != nil; child = child.NextSibling {
		clean(child)
	}
}

func removeChildren(n *html.Node) {
	for n.FirstChild != nil {
		n.RemoveChild(n.FirstChild)
	}
}

// structural attributes carry the information selectors are built from. Their
// values are kept verbatim — a class list can easily look like an opaque token,
// and redacting it would destroy exactly what discovery needs.
var structural = map[string]bool{
	"class": true, "id": true, "name": true, "type": true, "role": true,
	"placeholder": true, "for": true, "tabindex": true, "disabled": true,
	"readonly": true, "checked": true, "selected": true, "multiple": true,
	"maxlength": true, "minlength": true, "min": true, "max": true, "step": true,
	"colspan": true, "rowspan": true, "hidden": true, "lang": true, "dir": true,
}

func isStructural(key string) bool {
	return structural[key] || strings.HasPrefix(key, "data-") || strings.HasPrefix(key, "aria-")
}

func cleanAttrs(tag string, attrs []html.Attribute) []html.Attribute {
	out := make([]html.Attribute, 0, len(attrs))
	for _, a := range attrs {
		key := strings.ToLower(a.Key)

		// Inline event handlers are script; they never help with selectors.
		if strings.HasPrefix(key, "on") {
			continue
		}
		if key == "srcdoc" || key == "integrity" || key == "nonce" {
			continue
		}
		if logging.IsSecretKey(key) {
			a.Val = Redacted
			out = append(out, a)
			continue
		}
		if key == "value" && valueBearing[tag] {
			a.Val = Redacted
			out = append(out, a)
			continue
		}
		if isStructural(key) {
			out = append(out, a)
			continue
		}
		switch key {
		case "src", "href", "action", "poster", "data", "content":
			a.Val = stripURL(a.Val)
		default:
			a.Val = RedactText(a.Val)
		}
		out = append(out, a)
	}
	return out
}

// stripURL keeps the path, which can identify a page, and drops the query and
// fragment, which can carry tokens. Data URIs are dropped entirely.
func stripURL(v string) string {
	trimmed := strings.TrimSpace(v)
	if strings.HasPrefix(strings.ToLower(trimmed), "data:") {
		return Redacted
	}
	if i := strings.IndexAny(trimmed, "?#"); i >= 0 {
		return trimmed[:i] + "?" + Redacted
	}
	return RedactText(trimmed)
}

// RedactText removes personal and session data from a piece of page text while
// leaving prices, quantities and Persian labels intact.
func RedactText(s string) string {
	if strings.TrimSpace(s) == "" {
		return s
	}
	s = email.ReplaceAllString(s, Redacted)
	s = iranPhone.ReplaceAllString(s, Redacted)
	s = tokenish.ReplaceAllStringFunc(s, func(m string) string {
		// Long opaque strings are tokens; long words with spaces are not
		// matched by the pattern at all.
		return Redacted
	})
	s = redactIdentifiers(s)
	return s
}

// redactIdentifiers removes account numbers, card numbers and national IDs
// while leaving prices and quantities alone.
//
// The distinction is how the number is written: a price on a brokerage page is
// grouped in thousands ("33,100,000"), whereas an identifier is a bare run of
// ten or more digits ("6104337812345678"). Anything with twelve digits or more
// is treated as an identifier however it is written.
func redactIdentifiers(s string) string {
	return digitRun.ReplaceAllStringFunc(s, func(run string) string {
		digits := 0
		grouped := false
		for _, r := range run {
			switch {
			case r == ',' || r == '\u066C' || r == '\'':
				grouped = true
			default:
				digits++
			}
		}
		if digits >= 12 || (digits >= 10 && !grouped) {
			return Redacted
		}
		return run
	})
}
