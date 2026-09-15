package browser

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// SessionInfo describes the persistent Chromium profile, which is where the
// brokerage session lives between runs. Goroker never reads cookies or tokens
// out of the profile; it only reports whether one exists.
type SessionInfo struct {
	// Available is true when a profile directory with Chromium state exists.
	Available bool
	// Path is the profile directory.
	Path string
	// LastUsed is the modification time of the profile state.
	LastUsed time.Time
}

// InspectSession reports on the persistent profile at dir without launching a
// browser, so `goroker status` can answer quickly.
func InspectSession(dir string) (SessionInfo, error) {
	info := SessionInfo{Path: dir}
	if dir == "" {
		return info, nil
	}
	stat, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return info, nil
		}
		return info, fmt.Errorf("stat profile dir: %w", err)
	}
	if !stat.IsDir() {
		return info, fmt.Errorf("browser profile path %s is not a directory", dir)
	}

	// Chromium writes these once a profile has been used. Their presence is
	// what makes a restored session plausible; authentication itself is always
	// re-checked against the live site.
	markers := []string{
		filepath.Join(dir, "Default", "Preferences"),
		filepath.Join(dir, "Default", "Cookies"),
		filepath.Join(dir, "Local State"),
	}
	for _, m := range markers {
		st, err := os.Stat(m)
		if err != nil {
			continue
		}
		info.Available = true
		if st.ModTime().After(info.LastUsed) {
			info.LastUsed = st.ModTime()
		}
	}
	return info, nil
}
