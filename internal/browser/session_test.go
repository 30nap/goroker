package browser_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/30nap/goroker/internal/browser"
)

func TestInspectSessionMissingProfile(t *testing.T) {
	info, err := browser.InspectSession(filepath.Join(t.TempDir(), "no-profile"))
	if err != nil {
		t.Fatalf("InspectSession() = %v, want nil", err)
	}
	if info.Available {
		t.Fatal("a missing profile was reported as an available session")
	}
}

func TestInspectSessionWithProfile(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "Default"), 0o700); err != nil {
		t.Fatalf("create profile: %v", err)
	}
	for _, name := range []string{
		filepath.Join(dir, "Default", "Preferences"),
		filepath.Join(dir, "Local State"),
	} {
		if err := os.WriteFile(name, []byte("{}"), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	info, err := browser.InspectSession(dir)
	if err != nil {
		t.Fatalf("InspectSession() = %v, want nil", err)
	}
	if !info.Available {
		t.Fatal("a used profile was not reported as an available session")
	}
	if info.LastUsed.IsZero() {
		t.Fatal("LastUsed was not filled in")
	}
}

func TestInspectSessionRejectsAFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "profile")
	if err := os.WriteFile(path, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if _, err := browser.InspectSession(path); err == nil {
		t.Fatal("InspectSession() = nil, want an error for a non-directory profile path")
	}
}
