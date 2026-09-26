package plugin

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Past logCap the log rotates to plugin.log.1 and keeps writing to a
// fresh plugin.log, so a chatty plugin costs at most twice the cap.
func TestCappedLog_RotatesAtCap(t *testing.T) {
	old := logCap
	logCap = 10
	t.Cleanup(func() { logCap = old })

	path := filepath.Join(t.TempDir(), logFile)
	l, err := openCappedLog(path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()

	for _, s := range []string{"aaaaaaaa", "bbbbbbbb", "cc"} {
		if _, err := l.Write([]byte(s)); err != nil {
			t.Fatalf("write %q: %v", s, err)
		}
	}

	if got := readLog(t, path+".1"); got != "aaaaaaaa" {
		t.Errorf("plugin.log.1 = %q, want the pre-rotation output", got)
	}
	if got := readLog(t, path); got != "bbbbbbbbcc" {
		t.Errorf("plugin.log = %q, want the post-rotation output", got)
	}

	// A second rotation replaces the earlier generation, not appends to it.
	if _, err := l.Write([]byte("d")); err != nil {
		t.Fatal(err)
	}
	if got := readLog(t, path+".1"); got != "bbbbbbbbcc" {
		t.Errorf("after 2nd rotation plugin.log.1 = %q", got)
	}
	if got := readLog(t, path); got != "d" {
		t.Errorf("after 2nd rotation plugin.log = %q", got)
	}
}

// A reopen that fails after rotation leaves the log closed: later writes
// are swallowed rather than panicking on a nil file.
func TestCappedLog_ReopenFailureSwallowsLaterWrites(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("removing a file that is still open fails on windows")
	}
	old := logCap
	logCap = 4
	t.Cleanup(func() { logCap = old })

	dir := t.TempDir()
	path := filepath.Join(dir, logFile)
	l, err := openCappedLog(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	// Make the reopen fail: the rotated path is renamed away, and a
	// directory now occupies plugin.log.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	// Rename(dir, dir.1) succeeds on unix, so block it too.
	if err := os.Mkdir(path+".1", 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path+".1", "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := l.Write([]byte("defg")); err == nil {
		t.Fatal("write after a failed reopen: want an error")
	}
	if n, err := l.Write([]byte("late")); err != nil || n != 4 {
		t.Fatalf("late write = (%d, %v), want swallowed (4, nil)", n, err)
	}
	if err := l.Close(); err != nil {
		t.Fatalf("close after failed reopen: %v", err)
	}
}

func readLog(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
