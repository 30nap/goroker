package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
)

// Options configures the logger.
type Options struct {
	Level  string
	Format string
	Writer io.Writer
}

// New builds a slog.Logger that writes to opts.Writer with secret redaction
// always enabled.
func New(opts Options) (*slog.Logger, error) {
	level, err := parseLevel(opts.Level)
	if err != nil {
		return nil, err
	}
	handlerOpts := &slog.HandlerOptions{Level: level}

	var base slog.Handler
	switch strings.ToLower(opts.Format) {
	case "", "text":
		base = slog.NewTextHandler(opts.Writer, handlerOpts)
	case "json":
		base = slog.NewJSONHandler(opts.Writer, handlerOpts)
	default:
		return nil, fmt.Errorf("unknown log format %q", opts.Format)
	}
	return slog.New(&redactingHandler{inner: base}), nil
}

func parseLevel(name string) (slog.Level, error) {
	switch strings.ToLower(name) {
	case "", "info":
		return slog.LevelInfo, nil
	case "debug":
		return slog.LevelDebug, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("unknown log level %q", name)
	}
}

// Redacted is the placeholder written in place of a secret value.
const Redacted = "[REDACTED]"

// secretKeys are attribute keys whose values are never written to the log.
// Matching is case-insensitive and substring-based, so "broker_password" and
// "Authorization" are both caught.
var secretKeys = []string{
	"password", "passwd", "pass", "secret", "otp", "token", "cookie",
	"authorization", "auth_header", "credential", "session_id", "jwt",
	"api_key", "apikey", "pin", "captcha", "national_id", "localstorage",
}

// IsSecretKey reports whether an attribute key must be redacted.
func IsSecretKey(key string) bool {
	lower := strings.ToLower(key)
	for _, s := range secretKeys {
		if strings.Contains(lower, s) {
			return true
		}
	}
	return false
}

// redactingHandler wraps another slog.Handler and replaces the value of any
// attribute with a secret-looking key, at any nesting depth.
type redactingHandler struct {
	inner slog.Handler
}

func (h *redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *redactingHandler) Handle(ctx context.Context, rec slog.Record) error {
	clean := slog.NewRecord(rec.Time, rec.Level, rec.Message, rec.PC)
	rec.Attrs(func(a slog.Attr) bool {
		clean.AddAttrs(redactAttr(a))
		return true
	})
	return h.inner.Handle(ctx, clean)
}

func (h *redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Attr, 0, len(attrs))
	for _, a := range attrs {
		out = append(out, redactAttr(a))
	}
	return &redactingHandler{inner: h.inner.WithAttrs(out)}
}

func (h *redactingHandler) WithGroup(name string) slog.Handler {
	return &redactingHandler{inner: h.inner.WithGroup(name)}
}

func redactAttr(a slog.Attr) slog.Attr {
	if IsSecretKey(a.Key) {
		return slog.String(a.Key, Redacted)
	}
	if a.Value.Kind() == slog.KindGroup {
		group := a.Value.Group()
		out := make([]any, 0, len(group))
		for _, sub := range group {
			out = append(out, redactAttr(sub))
		}
		return slog.Group(a.Key, out...)
	}
	return a
}
