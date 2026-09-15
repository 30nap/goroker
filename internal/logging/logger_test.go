package logging_test

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/30nap/goroker/internal/logging"
	"github.com/30nap/goroker/internal/storage"
)

// secret values that must never reach the log, whatever attribute they are
// attached to.
const (
	secretPassword = "sup3r-s3cret-brokerage-pw"
	secretOTP      = "489210"
	secretCookie   = "SESSIONID=abc123def456"
	secretToken    = "eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.payload.signature"
)

// --- Safety test 5 ---------------------------------------------------------

// TestCredentialsNeverReachTheLog proves that credential-shaped attributes are
// redacted, in both formats and at any nesting depth.
func TestCredentialsNeverReachTheLog(t *testing.T) {
	for _, format := range []string{"text", "json"} {
		t.Run(format, func(t *testing.T) {
			var buf bytes.Buffer
			log, err := logging.New(logging.Options{Level: "debug", Format: format, Writer: &buf})
			if err != nil {
				t.Fatalf("New() = %v", err)
			}

			log.Info(logging.EventLoginSuccess,
				slog.String("username", "sina"),
				slog.String("password", secretPassword),
				slog.String("otp", secretOTP),
				slog.String("cookie", secretCookie),
				slog.String("authorization", "Bearer "+secretToken),
				slog.String("broker_password", secretPassword),
				slog.String("session_id", "s-123"),
			)
			log.With(slog.String("password", secretPassword)).
				Info(logging.EventAppStart)
			log.Info(logging.EventBrowserStarted,
				slog.Group("credentials",
					slog.String("token", secretToken),
					slog.String("api_key", "k-999"),
				),
			)

			out := buf.String()
			for _, secret := range []string{secretPassword, secretOTP, secretCookie, secretToken, "k-999", "s-123"} {
				if strings.Contains(out, secret) {
					t.Fatalf("log contains a secret value:\n%s", out)
				}
			}
			if !strings.Contains(out, logging.Redacted) {
				t.Fatalf("log does not show the redaction placeholder:\n%s", out)
			}
			// Non-secret context must survive redaction.
			if !strings.Contains(out, "sina") {
				t.Fatalf("redaction removed a non-secret attribute:\n%s", out)
			}
		})
	}
}

// TestCredentialsStructIsNotPrintable proves that passing a Credentials value
// around cannot leak the password through formatting or logging.
func TestCredentialsStructIsNotPrintable(t *testing.T) {
	creds := storage.Credentials{Username: "sina", Password: secretPassword}

	if strings.Contains(creds.String(), secretPassword) {
		t.Fatal("Credentials.String() exposes the password")
	}

	var buf bytes.Buffer
	log, err := logging.New(logging.Options{Level: "debug", Format: "text", Writer: &buf})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	log.Info(logging.EventLoginSuccess, slog.Any("creds", creds))
	if strings.Contains(buf.String(), secretPassword) {
		t.Fatalf("logging a Credentials value exposed the password:\n%s", buf.String())
	}
}

func TestIsSecretKey(t *testing.T) {
	secret := []string{"password", "Password", "broker_password", "otp", "OTP_CODE", "cookie", "authorization", "token", "api_key", "captcha_answer"}
	for _, key := range secret {
		if !logging.IsSecretKey(key) {
			t.Errorf("IsSecretKey(%q) = false, want true", key)
		}
	}
	public := []string{"symbol", "price", "quantity", "market", "state", "url", "username", "interval"}
	for _, key := range public {
		if logging.IsSecretKey(key) {
			t.Errorf("IsSecretKey(%q) = true, want false", key)
		}
	}
}

func TestLevelsAndFormats(t *testing.T) {
	var buf bytes.Buffer
	log, err := logging.New(logging.Options{Level: "warn", Format: "json", Writer: &buf})
	if err != nil {
		t.Fatalf("New() = %v", err)
	}
	log.Debug(logging.EventQuoteRead)
	if buf.Len() != 0 {
		t.Fatalf("debug line emitted at warn level: %s", buf.String())
	}
	log.Warn(logging.EventMarketUnknown)
	if !strings.Contains(buf.String(), logging.EventMarketUnknown) {
		t.Fatalf("warn line missing: %s", buf.String())
	}

	if _, err := logging.New(logging.Options{Level: "chatty", Writer: &buf}); err == nil {
		t.Fatal("New() accepted an unknown level")
	}
	if _, err := logging.New(logging.Options{Level: "info", Format: "xml", Writer: &buf}); err == nil {
		t.Fatal("New() accepted an unknown format")
	}
}
