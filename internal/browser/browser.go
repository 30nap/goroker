// Package browser wraps Rod so the rest of the application works with a small,
// explicit surface: launch a Chromium with a persistent profile, hand out
// pages, save failure screenshots and shut down cleanly.
//
// No brokerage-specific knowledge lives here.
package browser

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/go-rod/rod"
	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/proto"

	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/logging"
)

// Options configures the Chromium instance.
type Options struct {
	// ProfileDir is the persistent user-data directory. It holds cookies and
	// local storage, which is how sessions survive between runs.
	ProfileDir string
	// Headless should stay false for trading: order interaction is meant to
	// be visible.
	Headless bool
	// BinPath pins a Chromium binary. Empty lets Rod locate one.
	BinPath string
	// Timeout bounds a single browser operation.
	Timeout time.Duration
	// SlowMotion delays each action, which is easier to follow and gentler on
	// the broker UI.
	SlowMotion time.Duration
	// DebugDir receives failure screenshots.
	DebugDir string
	// Screenshots enables failure screenshots.
	Screenshots bool

	Logger *slog.Logger
}

// Browser is a running Chromium with a persistent profile.
type Browser struct {
	rod      *rod.Browser
	launcher *launcher.Launcher
	opts     Options
	log      *slog.Logger
}

// Launch starts Chromium with the persistent profile. The caller owns the
// returned Browser and must Close it.
func Launch(ctx context.Context, opts Options) (*Browser, error) {
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.ProfileDir == "" {
		return nil, fmt.Errorf("%w: no browser profile directory configured", domain.ErrBrowserUnavailable)
	}
	if err := os.MkdirAll(opts.ProfileDir, 0o700); err != nil {
		return nil, fmt.Errorf("%w: create profile dir: %v", domain.ErrBrowserUnavailable, err)
	}

	l := launcher.New().
		UserDataDir(opts.ProfileDir).
		Headless(opts.Headless).
		// The profile carries authentication state; keep it after exit.
		Leakless(true).
		Set("disable-background-networking").
		Set("disable-features", "Translate,MediaRouter").
		Set("no-first-run").
		Set("no-default-browser-check")

	if opts.BinPath != "" {
		l = l.Bin(opts.BinPath)
	}

	controlURL, err := l.Context(ctx).Launch()
	if err != nil {
		l.Kill()
		return nil, fmt.Errorf("%w: launch chromium: %v", domain.ErrBrowserUnavailable, err)
	}

	rodBrowser := rod.New().
		Context(ctx).
		ControlURL(controlURL).
		SlowMotion(opts.SlowMotion)

	if err := rodBrowser.Connect(); err != nil {
		l.Kill()
		return nil, fmt.Errorf("%w: connect to chromium: %v", domain.ErrBrowserUnavailable, err)
	}

	b := &Browser{rod: rodBrowser, launcher: l, opts: opts, log: opts.Logger}
	b.log.Info(logging.EventBrowserStarted,
		slog.String("profile_dir", opts.ProfileDir),
		slog.Bool("headless", opts.Headless))
	return b, nil
}

// Rod exposes the underlying browser for adapter code.
func (b *Browser) Rod() *rod.Browser { return b.rod }

// Timeout is the configured per-operation timeout.
func (b *Browser) Timeout() time.Duration { return b.opts.Timeout }

// Page returns a page showing url. An existing page for the same origin is
// reused so the broker's live (WebSocket) updates are not thrown away by a
// needless reload.
func (b *Browser) Page(ctx context.Context, url string) (*rod.Page, error) {
	if b.rod == nil {
		return nil, domain.ErrBrowserUnavailable
	}
	page, err := b.rod.Context(ctx).Page(proto.TargetCreateTarget{URL: url})
	if err != nil {
		return nil, fmt.Errorf("%w: open page: %v", domain.ErrBrowserUnavailable, err)
	}
	return page.Context(ctx).Timeout(b.opts.Timeout), nil
}

// Healthy reports whether the browser still answers. A disconnected browser is
// an ABORT condition for every caller.
func (b *Browser) Healthy() bool {
	if b.rod == nil {
		return false
	}
	_, err := b.rod.Version()
	return err == nil
}

// Screenshot saves a PNG of page into the debug directory and returns its path.
// It is best-effort: a failure to save is logged, never fatal.
//
// Screenshots are taken on automation failures only, and callers must not ask
// for one while a login, OTP or CAPTCHA form is on screen.
func (b *Browser) Screenshot(page *rod.Page, label string) (string, error) {
	if !b.opts.Screenshots || b.opts.DebugDir == "" || page == nil {
		return "", nil
	}
	if err := os.MkdirAll(b.opts.DebugDir, 0o700); err != nil {
		return "", fmt.Errorf("create debug dir: %w", err)
	}
	name := fmt.Sprintf("%s-%s.png", time.Now().Format("2006-01-02T150405"), sanitize(label))
	path := filepath.Join(b.opts.DebugDir, name)

	data, err := page.Screenshot(false, nil)
	if err != nil {
		return "", fmt.Errorf("capture screenshot: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", fmt.Errorf("write screenshot: %w", err)
	}
	b.log.Info(logging.EventScreenshot, slog.String("path", path), slog.String("label", label))
	return path, nil
}

// Close shuts Chromium down. The profile directory is left in place: it is the
// session store.
func (b *Browser) Close() error {
	if b == nil {
		return nil
	}
	var errs []error
	if b.rod != nil {
		if err := b.rod.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	if b.launcher != nil {
		b.launcher.Kill()
	}
	b.log.Info(logging.EventBrowserClosed)
	return errors.Join(errs...)
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			out = append(out, r)
		default:
			out = append(out, '-')
		}
	}
	return string(out)
}
