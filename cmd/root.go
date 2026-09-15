// Package cmd wires Goroker's command line. Commands parse input, build the
// services they need, and print results; all rules live in the application and
// domain packages.
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/30nap/goroker/internal/application"
	"github.com/30nap/goroker/internal/broker"
	_ "github.com/30nap/goroker/internal/broker/iranbroker" // register the adapter
	"github.com/30nap/goroker/internal/browser"
	"github.com/30nap/goroker/internal/config"
	"github.com/30nap/goroker/internal/domain"
	"github.com/30nap/goroker/internal/logging"
	"github.com/30nap/goroker/internal/storage"
	"log/slog"
)

// Version is set at build time.
var Version = "0.1.0-phase1"

type globalFlags struct {
	configPath string
	logLevel   string
	logFormat  string
	headless   bool
}

var flags globalFlags

// NewRootCommand builds the `goroker` command tree.
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:   "goroker",
		Short: "Browser-automation assistant for an Iranian stock brokerage account",
		Long: `Goroker drives your brokerage's web UI through Chromium.

It can sign in, restore a session, read live quotes, watch for a target price
and prepare a BUY order. It never submits an order without an explicit
interactive confirmation typed immediately before submission.`,
		SilenceUsage:  true,
		SilenceErrors: true,
		Version:       Version,
	}

	root.PersistentFlags().StringVar(&flags.configPath, "config", "", "path to config.yaml (default ~/.goroker/config.yaml)")
	root.PersistentFlags().StringVar(&flags.logLevel, "log-level", "", "log level: debug, info, warn, error")
	root.PersistentFlags().StringVar(&flags.logFormat, "log-format", "", "log format: text or json")
	root.PersistentFlags().BoolVar(&flags.headless, "headless", false, "run Chromium headless (not recommended for trading)")

	root.AddCommand(
		newLoginCommand(),
		newStatusCommand(),
		newQuoteCommand(),
		newWatchCommand(),
		newBuyCommand(),
	)
	return root
}

// Execute runs the CLI with the given cancellable context.
func Execute(ctx context.Context) int {
	root := NewRootCommand()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, formatError(err))
		return 1
	}
	return 0
}

// formatError renders an error as an operator-facing ABORT line. Goroker fails
// closed, so every error means nothing was submitted.
func formatError(err error) string {
	switch {
	case errors.Is(err, domain.ErrCancelled):
		return "ABORT: cancelled. No order was submitted."
	case errors.Is(err, domain.ErrNotConfirmed), errors.Is(err, domain.ErrConfirmationExpire):
		return fmt.Sprintf("ABORT: %v. No order was submitted.", err)
	default:
		return fmt.Sprintf("ABORT: %v", err)
	}
}

// App holds the objects a command needs. It owns the browser and the adapter.
type App struct {
	Cfg     config.Config
	Log     *slog.Logger
	Browser *browser.Browser
	Adapter broker.Adapter
	Creds   storage.Store
}

// loadApp reads configuration and builds the logger without touching the
// browser, for commands that do not need one.
func loadApp() (*App, error) {
	cfg, err := config.Load(flags.configPath)
	if err != nil {
		return nil, err
	}
	if flags.logLevel != "" {
		cfg.Logging.Level = flags.logLevel
	}
	if flags.logFormat != "" {
		cfg.Logging.Format = flags.logFormat
	}
	if flags.headless {
		cfg.Browser.Headless = true
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if err := cfg.EnsureDirs(); err != nil {
		return nil, err
	}

	log, err := logging.New(logging.Options{
		Level:  cfg.Logging.Level,
		Format: cfg.Logging.Format,
		Writer: os.Stderr,
	})
	if err != nil {
		return nil, err
	}
	log.Info(logging.EventAppStart, slog.String("version", Version), slog.String("broker", cfg.Broker.Name))

	return &App{Cfg: cfg, Log: log, Creds: storage.NewKeyringStore()}, nil
}

// OpenBrowser launches Chromium with the persistent profile and builds the
// broker adapter.
func (a *App) OpenBrowser(ctx context.Context) error {
	b, err := browser.Launch(ctx, browser.Options{
		ProfileDir:  a.Cfg.Browser.ProfileDir,
		Headless:    a.Cfg.Browser.Headless,
		BinPath:     a.Cfg.Browser.BinPath,
		Timeout:     a.Cfg.Browser.Timeout,
		SlowMotion:  a.Cfg.Browser.SlowMotion,
		DebugDir:    a.Cfg.Debug.Dir,
		Screenshots: a.Cfg.Debug.Screenshots,
		Logger:      a.Log,
	})
	if err != nil {
		return err
	}
	a.Browser = b

	adapter, err := broker.New(broker.Deps{
		Config:      a.Cfg,
		Browser:     b,
		Credentials: a.Creds,
		Logger:      a.Log,
	})
	if err != nil {
		_ = b.Close()
		a.Browser = nil
		return err
	}
	a.Adapter = adapter
	return nil
}

// Close shuts everything down. It is safe to call more than once.
func (a *App) Close() {
	if a.Adapter != nil {
		_ = a.Adapter.Close()
		a.Adapter = nil
	}
	if a.Browser != nil {
		_ = a.Browser.Close()
		a.Browser = nil
	}
}

// withBrowser runs fn with a launched browser and an adapter, and always closes
// them, including when the context is cancelled.
func withBrowser(cmd *cobra.Command, fn func(ctx context.Context, app *App) error) error {
	app, err := loadApp()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	if err := app.OpenBrowser(ctx); err != nil {
		return err
	}
	defer app.Close()
	return fn(ctx, app)
}

// quoteService is a small helper used by several commands.
func (a *App) quoteService() *application.QuoteService {
	return application.NewQuoteService(a.Adapter, a.Cfg.Watch.MaxQuoteAge, a.Log, nil)
}
