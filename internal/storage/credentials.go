// Package storage holds Goroker's credential access. Credentials are never
// written to the configuration file, never logged, and never passed on the
// command line: they come from the OS keyring or from the environment, and
// they are read only at the moment the login form is filled.
package storage

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
)

// KeyringService is the service name Goroker stores credentials under.
const KeyringService = "goroker"

// Environment variables understood for development and for setups without a
// usable keyring (headless servers, some Linux desktops).
const (
	EnvUsername = "GOROKER_USERNAME"
	EnvPassword = "GOROKER_PASSWORD"
)

// ErrNoCredentials means no username/password pair is available. It is not
// fatal: `goroker login` falls back to fully manual login in that case.
var ErrNoCredentials = errors.New("no stored credentials")

// Credentials is a username/password pair held in memory only.
type Credentials struct {
	Username string
	Password string
}

// Source describes where a credential came from, for logging. The value itself
// is never logged.
type Source string

const (
	SourceKeyring Source = "keyring"
	SourceEnv     Source = "environment"
	SourceNone    Source = "none"
)

// String deliberately hides the password so that accidentally printing a
// Credentials value cannot leak it.
func (c Credentials) String() string {
	return fmt.Sprintf("Credentials{Username:%q, Password:[REDACTED]}", c.Username)
}

// LogValue keeps the password out of structured logs even if a Credentials
// value is passed to slog directly.
func (c Credentials) LogValue() any {
	return "[REDACTED]"
}

// Store is where credentials are read from and written to.
type Store interface {
	Load(account string) (Credentials, Source, error)
	Save(account string, creds Credentials) error
	Delete(account string) error
}

// KeyringStore reads the OS keyring first and falls back to the environment.
type KeyringStore struct{}

// NewKeyringStore returns the default credential store.
func NewKeyringStore() *KeyringStore { return &KeyringStore{} }

// Load returns the credentials for account, preferring the OS keyring.
// account is a label chosen by the user (typically the broker name), not the
// brokerage username.
func (s *KeyringStore) Load(account string) (Credentials, Source, error) {
	user := strings.TrimSpace(os.Getenv(EnvUsername))
	pass := os.Getenv(EnvPassword)
	if user != "" && pass != "" {
		return Credentials{Username: user, Password: pass}, SourceEnv, nil
	}

	secret, err := keyring.Get(KeyringService, account)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return Credentials{}, SourceNone, ErrNoCredentials
		}
		return Credentials{}, SourceNone, fmt.Errorf("read keyring: %w", err)
	}
	creds, err := decode(secret)
	if err != nil {
		return Credentials{}, SourceNone, err
	}
	return creds, SourceKeyring, nil
}

// Save writes credentials to the OS keyring.
func (s *KeyringStore) Save(account string, creds Credentials) error {
	if creds.Username == "" || creds.Password == "" {
		return errors.New("username and password are both required")
	}
	if strings.Contains(creds.Username, "\x00") {
		return errors.New("username contains an invalid character")
	}
	if err := keyring.Set(KeyringService, account, encode(creds)); err != nil {
		return fmt.Errorf("write keyring: %w", err)
	}
	return nil
}

// Delete removes the stored credentials for account.
func (s *KeyringStore) Delete(account string) error {
	if err := keyring.Delete(KeyringService, account); err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return ErrNoCredentials
		}
		return fmt.Errorf("delete from keyring: %w", err)
	}
	return nil
}

// encode/decode keep the pair in a single keyring entry. A NUL byte separates
// the fields because it cannot occur in either of them.
func encode(c Credentials) string { return c.Username + "\x00" + c.Password }

func decode(raw string) (Credentials, error) {
	user, pass, ok := strings.Cut(raw, "\x00")
	if !ok {
		return Credentials{}, errors.New("stored credential is malformed")
	}
	return Credentials{Username: user, Password: pass}, nil
}
