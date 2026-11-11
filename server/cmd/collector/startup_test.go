package main

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Failures before the log exists must name the setting to fix: running the
// binary without configuration used to print only a pointer to a log file that
// was never created.
func TestStartupErrorsNameTheSetting(t *testing.T) {
	t.Run("missing secret key", func(t *testing.T) {
		t.Setenv("LIVETAP_SECRET_KEY", "")
		var early startupError
		if err := run(); !errors.As(err, &early) || !strings.Contains(err.Error(), "LIVETAP_SECRET_KEY") {
			t.Fatalf("want a startup error about LIVETAP_SECRET_KEY, got %v", err)
		}
	})
	t.Run("unwritable data directory", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "not-a-directory")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("LIVETAP_SECRET_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
		t.Setenv("LIVETAP_DATA_DIR", file)
		var early startupError
		if err := run(); !errors.As(err, &early) || !strings.Contains(err.Error(), "LIVETAP_DATA_DIR") {
			t.Fatalf("want a startup error about LIVETAP_DATA_DIR, got %v", err)
		}
	})
}
