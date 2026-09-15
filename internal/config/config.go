// Package config loads Goroker's configuration from ~/.goroker/config.yaml and
// lets environment variables override it. It never holds credentials: those
// come from the OS keyring or from the environment at the moment they are used.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/providers/structs"
	"github.com/knadh/koanf/v2"
)

// AppDirName is the per-user application directory under $HOME.
const AppDirName = ".goroker"

// Config is the full application configuration.
type Config struct {
	Broker  BrokerConfig  `koanf:"broker"`
	Browser BrowserConfig `koanf:"browser"`
	Watch   WatchConfig   `koanf:"watch"`
	Order   OrderConfig   `koanf:"order"`
	Logging LoggingConfig `koanf:"logging"`
	Debug   DebugConfig   `koanf:"debug"`

	// Dir is the resolved application directory (~/.goroker). It is derived,
	// not read from the file.
	Dir string `koanf:"-"`
}

// BrokerConfig identifies the brokerage adapter and its entry points. The URLs
// are configuration rather than constants because they are specific to the
// user's brokerage and are filled in during broker discovery.
type BrokerConfig struct {
	// Name selects the adapter implementation.
	Name string `koanf:"name"`
	// DisplayName is shown by `goroker status`.
	DisplayName string `koanf:"display_name"`
	// BaseURL is the brokerage site root.
	BaseURL string `koanf:"base_url"`
	// LoginURL is the login page.
	LoginURL string `koanf:"login_url"`
	// TradingURL is the trading panel.
	TradingURL string `koanf:"trading_url"`
}

// BrowserConfig controls the Chromium instance.
type BrowserConfig struct {
	// Headless defaults to false: trading interaction should be visible.
	Headless bool `koanf:"headless"`
	// ProfileDir is the persistent Chromium profile directory. Empty means
	// <app dir>/browser-profile.
	ProfileDir string `koanf:"profile_dir"`
	// BinPath optionally pins a Chromium binary instead of the one Rod finds.
	BinPath string `koanf:"bin_path"`
	// Timeout bounds any single browser operation.
	Timeout time.Duration `koanf:"timeout"`
	// SlowMotion adds a delay between browser actions, which makes the
	// automation easier to follow and gentler on the broker UI.
	SlowMotion time.Duration `koanf:"slow_motion"`
	// ManualChallengeTimeout is how long Goroker waits while the user solves
	// an OTP/CAPTCHA/device challenge by hand.
	ManualChallengeTimeout time.Duration `koanf:"manual_challenge_timeout"`
}

// WatchConfig controls price monitoring.
type WatchConfig struct {
	// Interval between quote reads. Kept deliberately gentle.
	Interval time.Duration `koanf:"interval"`
	// MaxQuoteAge is the oldest a quote may be and still support a decision.
	MaxQuoteAge time.Duration `koanf:"max_quote_age"`
	// Timeout bounds a whole watch/buy wait. Zero means wait indefinitely
	// until the user interrupts.
	Timeout time.Duration `koanf:"timeout"`
}

// OrderConfig controls order preparation and confirmation.
type OrderConfig struct {
	// ConfirmationTimeout is how long the confirmation prompt stays valid.
	ConfirmationTimeout time.Duration `koanf:"confirmation_timeout"`
}

// LoggingConfig controls structured logging.
type LoggingConfig struct {
	// Level is one of debug, info, warn, error.
	Level string `koanf:"level"`
	// Format is "text" or "json".
	Format string `koanf:"format"`
}

// DebugConfig controls failure artefacts.
type DebugConfig struct {
	// Screenshots enables saving a screenshot when UI automation fails.
	Screenshots bool `koanf:"screenshots"`
	// Dir is where those screenshots go. Empty means <app dir>/debug.
	Dir string `koanf:"dir"`
}

// Default returns the built-in configuration.
func Default() Config {
	return Config{
		Broker: BrokerConfig{
			Name:        "unknown",
			DisplayName: "unknown",
		},
		Browser: BrowserConfig{
			Headless:               false,
			Timeout:                30 * time.Second,
			SlowMotion:             0,
			ManualChallengeTimeout: 5 * time.Minute,
		},
		Watch: WatchConfig{
			Interval:    2 * time.Second,
			MaxQuoteAge: 5 * time.Second,
		},
		Order: OrderConfig{
			ConfirmationTimeout: 15 * time.Second,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "text",
		},
		Debug: DebugConfig{
			Screenshots: true,
		},
	}
}

// AppDir returns ~/.goroker, honouring GOROKER_HOME for tests and unusual setups.
func AppDir() (string, error) {
	if custom := os.Getenv("GOROKER_HOME"); custom != "" {
		return custom, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, AppDirName), nil
}

// Path returns the configuration file path inside the application directory.
func Path() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.yaml"), nil
}

// Load reads the configuration. A missing file is not an error: the defaults
// apply. Environment variables prefixed with GOROKER_ override file values,
// e.g. GOROKER_BROWSER_HEADLESS=true or GOROKER_WATCH_INTERVAL=5s.
func Load(path string) (Config, error) {
	cfg := Default()

	dir, err := AppDir()
	if err != nil {
		return cfg, err
	}
	cfg.Dir = dir

	if path == "" {
		path = filepath.Join(dir, "config.yaml")
	}

	k := koanf.New(".")
	if err := k.Load(structs.Provider(cfg, "koanf"), nil); err != nil {
		return cfg, fmt.Errorf("load defaults: %w", err)
	}

	if _, statErr := os.Stat(path); statErr == nil {
		if err := k.Load(file.Provider(path), yaml.Parser()); err != nil {
			return cfg, fmt.Errorf("read %s: %w", path, err)
		}
	} else if !os.IsNotExist(statErr) {
		return cfg, fmt.Errorf("stat %s: %w", path, statErr)
	}

	// GOROKER_WATCH_INTERVAL -> watch.interval
	envProvider := env.Provider("GOROKER_", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "GOROKER_")), "_", ".")
	})
	if err := k.Load(envProvider, nil); err != nil {
		return cfg, fmt.Errorf("load environment overrides: %w", err)
	}

	if err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{Tag: "koanf", DecoderConfig: decoderConfig(&cfg)}); err != nil {
		return cfg, fmt.Errorf("decode configuration: %w", err)
	}

	cfg.Dir = dir
	cfg.applyDerivedPaths()
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

func (c *Config) applyDerivedPaths() {
	if c.Browser.ProfileDir == "" {
		c.Browser.ProfileDir = filepath.Join(c.Dir, "browser-profile")
	}
	if c.Debug.Dir == "" {
		c.Debug.Dir = filepath.Join(c.Dir, "debug")
	}
	if c.Broker.DisplayName == "" {
		c.Broker.DisplayName = c.Broker.Name
	}
}

// Validate rejects configurations that would make safe trading impossible.
func (c Config) Validate() error {
	if c.Watch.Interval <= 0 {
		return fmt.Errorf("watch.interval must be positive, got %s", c.Watch.Interval)
	}
	if c.Watch.Interval < 500*time.Millisecond {
		return fmt.Errorf("watch.interval %s is too aggressive; use 500ms or more", c.Watch.Interval)
	}
	if c.Watch.MaxQuoteAge <= 0 {
		return fmt.Errorf("watch.max_quote_age must be positive, got %s", c.Watch.MaxQuoteAge)
	}
	if c.Order.ConfirmationTimeout <= 0 {
		return fmt.Errorf("order.confirmation_timeout must be positive, got %s", c.Order.ConfirmationTimeout)
	}
	switch c.Logging.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("logging.level must be one of debug|info|warn|error, got %q", c.Logging.Level)
	}
	switch c.Logging.Format {
	case "text", "json":
	default:
		return fmt.Errorf("logging.format must be text or json, got %q", c.Logging.Format)
	}
	return nil
}

// EnsureDirs creates the application directories with owner-only permissions.
// The browser profile holds authentication state, so it must not be readable
// by other users.
func (c Config) EnsureDirs() error {
	for _, dir := range []string{c.Dir, c.Browser.ProfileDir, c.Debug.Dir} {
		if dir == "" {
			continue
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return nil
}
