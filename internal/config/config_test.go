package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/30nap/goroker/internal/config"
)

func TestDefaults(t *testing.T) {
	t.Setenv("GOROKER_HOME", t.TempDir())

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.Browser.Headless {
		t.Fatal("headless must default to false: trading interaction should be visible")
	}
	if cfg.Watch.Interval != 2*time.Second {
		t.Fatalf("watch.interval = %s, want 2s", cfg.Watch.Interval)
	}
	if cfg.Watch.MaxQuoteAge != 5*time.Second {
		t.Fatalf("watch.max_quote_age = %s, want 5s", cfg.Watch.MaxQuoteAge)
	}
	if cfg.Order.ConfirmationTimeout != 15*time.Second {
		t.Fatalf("order.confirmation_timeout = %s, want 15s", cfg.Order.ConfirmationTimeout)
	}
	if filepath.Base(cfg.Browser.ProfileDir) != "browser-profile" {
		t.Fatalf("profile dir = %s, want it under the app directory", cfg.Browser.ProfileDir)
	}
	if filepath.Base(cfg.Debug.Dir) != "debug" {
		t.Fatalf("debug dir = %s, want <app dir>/debug", cfg.Debug.Dir)
	}
}

func TestLoadFileAndEnvironmentOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOROKER_HOME", home)

	path := filepath.Join(home, "config.yaml")
	content := `
broker:
  name: mofid
  display_name: My Broker
  base_url: https://example.invalid/
browser:
  headless: false
watch:
  interval: 3s
  max_quote_age: 4s
order:
  confirmation_timeout: 20s
logging:
  level: debug
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := config.Load(path)
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if cfg.Broker.Name != "mofid" || cfg.Broker.DisplayName != "My Broker" {
		t.Fatalf("broker = %+v, want the file values", cfg.Broker)
	}
	if cfg.Watch.Interval != 3*time.Second {
		t.Fatalf("watch.interval = %s, want 3s", cfg.Watch.Interval)
	}
	if cfg.Logging.Level != "debug" {
		t.Fatalf("logging.level = %s, want debug", cfg.Logging.Level)
	}

	// Environment variables override the file.
	t.Setenv("GOROKER_WATCH_INTERVAL", "7s")
	t.Setenv("GOROKER_BROWSER_HEADLESS", "true")
	t.Setenv("GOROKER_LOGGING_LEVEL", "warn")

	cfg, err = config.Load(path)
	if err != nil {
		t.Fatalf("Load() with environment = %v", err)
	}
	if cfg.Watch.Interval != 7*time.Second {
		t.Fatalf("watch.interval = %s, want the environment value 7s", cfg.Watch.Interval)
	}
	if !cfg.Browser.Headless {
		t.Fatal("browser.headless was not overridden by the environment")
	}
	if cfg.Logging.Level != "warn" {
		t.Fatalf("logging.level = %s, want warn", cfg.Logging.Level)
	}
}

func TestValidateRejectsUnsafeValues(t *testing.T) {
	base := config.Default()
	base.Dir = t.TempDir()

	cases := []struct {
		name   string
		mutate func(*config.Config)
	}{
		{"zero watch interval", func(c *config.Config) { c.Watch.Interval = 0 }},
		{"aggressive polling", func(c *config.Config) { c.Watch.Interval = 50 * time.Millisecond }},
		{"zero max quote age", func(c *config.Config) { c.Watch.MaxQuoteAge = 0 }},
		{"zero confirmation timeout", func(c *config.Config) { c.Order.ConfirmationTimeout = 0 }},
		{"unknown log level", func(c *config.Config) { c.Logging.Level = "loud" }},
		{"unknown log format", func(c *config.Config) { c.Logging.Format = "xml" }},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base
			tc.mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() = nil, want an error")
			}
		})
	}
}

func TestEnsureDirsIsOwnerOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("GOROKER_HOME", home)

	cfg, err := config.Load("")
	if err != nil {
		t.Fatalf("Load() = %v", err)
	}
	if err := cfg.EnsureDirs(); err != nil {
		t.Fatalf("EnsureDirs() = %v", err)
	}

	// The browser profile holds authentication state and must not be readable
	// by other users on the machine.
	info, err := os.Stat(cfg.Browser.ProfileDir)
	if err != nil {
		t.Fatalf("stat profile dir: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("profile dir mode = %o, want 700", perm)
	}
}
