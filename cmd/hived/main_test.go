package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestRemovePidfile(t *testing.T) {
	capture := func(t *testing.T) *bytes.Buffer {
		t.Helper()
		var buf bytes.Buffer
		orig := log.Writer()
		log.SetOutput(&buf)
		t.Cleanup(func() { log.SetOutput(orig) })
		return &buf
	}

	t.Run("removes existing file silently", func(t *testing.T) {
		buf := capture(t)
		path := filepath.Join(t.TempDir(), "hived.sock.pid")
		if err := os.WriteFile(path, []byte("123"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		removePidfile(path)
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Errorf("pidfile still exists after removePidfile")
		}
		if got := buf.String(); got != "" {
			t.Errorf("expected no log output; got %q", got)
		}
	})

	t.Run("missing file is silent", func(t *testing.T) {
		buf := capture(t)
		removePidfile(filepath.Join(t.TempDir(), "never-existed.pid"))
		if got := buf.String(); got != "" {
			t.Errorf("ErrNotExist should be suppressed (double shutdown is normal); got %q", got)
		}
	})

	t.Run("undeletable file logs a warning", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("chmod-based failure injection requires POSIX permissions")
		}
		if os.Getuid() == 0 {
			t.Skip("root bypasses permission bits")
		}
		buf := capture(t)
		dir := filepath.Join(t.TempDir(), "locked")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		path := filepath.Join(dir, "hived.sock.pid")
		if err := os.WriteFile(path, []byte("123"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatalf("Chmod: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		removePidfile(path)
		if !strings.Contains(buf.String(), "remove pidfile") {
			t.Errorf("expected 'remove pidfile' warning; got %q", buf.String())
		}
	})
}
