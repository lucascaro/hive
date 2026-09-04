package main

import (
	"bytes"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
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
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
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

	// The pidfile is the GUI restart path's fallback handle on the
	// daemon. A daemon exiting after its replacement is already up
	// must not delete the live daemon's pidfile — doing so leaves
	// Restart Hive with nothing to signal.
	t.Run("foreign pid is left alone", func(t *testing.T) {
		buf := capture(t)
		path := filepath.Join(t.TempDir(), "hived.sock.pid")
		if err := os.WriteFile(path, []byte("999999"), 0o600); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}
		removePidfile(path)
		if _, err := os.Stat(path); err != nil {
			t.Errorf("pidfile owned by another pid was removed: %v", err)
		}
		if !strings.Contains(buf.String(), "owned by pid 999999") {
			t.Errorf("expected an ownership log line; got %q", buf.String())
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
		if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
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

// TestPrintIdentityJSON pins the shape the GUI's updater parses out of
// a staged bundle. This is the only way to ask a hived on disk what
// its daemon contract is, and getting it wrong makes the updater fall
// back to a full restart — killing sessions it did not have to.
func TestPrintIdentityJSON(t *testing.T) {
	t.Cleanup(buildinfo.SetForTest("abc1234"))
	t.Cleanup(buildinfo.SetVersionForTest("0.9.9"))

	var buf bytes.Buffer
	printIdentity(&buf, true)

	var id buildinfo.Identity
	if err := json.Unmarshal(buf.Bytes(), &id); err != nil {
		t.Fatalf("unmarshal %q: %v", buf.String(), err)
	}
	if id.BuildID != "abc1234" || id.Release != "0.9.9" {
		t.Errorf("identity = %+v", id)
	}
	if id.DaemonContract != buildinfo.DaemonContract {
		t.Errorf("DaemonContract = %d, want %d", id.DaemonContract, buildinfo.DaemonContract)
	}
}

// The human form must stay human: a developer running `hived --version`
// should not get a JSON blob.
func TestPrintIdentityHuman(t *testing.T) {
	t.Cleanup(buildinfo.SetForTest("abc1234"))
	t.Cleanup(buildinfo.SetVersionForTest("0.9.9"))

	var buf bytes.Buffer
	printIdentity(&buf, false)

	out := buf.String()
	for _, want := range []string{"hived", "0.9.9", "abc1234", "daemon contract"} {
		if !strings.Contains(out, want) {
			t.Errorf("%q missing from %q", want, out)
		}
	}
	if strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("human form emitted JSON: %q", out)
	}
}
