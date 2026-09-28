//go:build e2e

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/wire/testclient"
)

// TestE2E_LayaClassifiesARealSession drives a real, isolated hived with
// Laya switched on against a real Laya server (spec 458): a new screen
// is classified within the spec's 3s — even after a Laya wait on the
// screen before it — the result is Laya-sourced, and the capture is
// private.
//
// Needs a running server, so it is opt-in:
//
//	HIVE_LAYA_URL=http://127.0.0.1:8001 go test -tags e2e ./cmd/hived/ -run Laya -v
//
// Which state the model picks is not asserted — that is accuracy, scored
// by internal/laya's TestCorpusAccuracy — only that the pipeline
// delivers a classification of the right screen in time.
func TestE2E_LayaClassifiesARealSession(t *testing.T) {
	url := os.Getenv("HIVE_LAYA_URL")
	if url == "" {
		t.Skip("HIVE_LAYA_URL not set; no Laya server")
	}
	skipIfWindows(t)
	tmp, err := os.MkdirTemp("/tmp", "hived-laya-e2e")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })
	sock, state, capDir := filepath.Join(tmp, "h.sock"), filepath.Join(tmp, "state"), filepath.Join(tmp, "cap")
	t.Setenv("HIVE_SOCKET", sock)
	t.Setenv("HIVE_STATE_DIR", state)
	t.Setenv("HOME", tmp)
	t.Setenv("HIVE_LAYA_CAPTURE_DIR", capDir)
	if err := testclient.RequireIsolation(); err != nil {
		t.Fatalf("isolation guard: %v", err)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(state, "agent-settings.json"),
		[]byte(`{"laya_enabled": true, "laya_url": "`+url+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	d := startBinary(t, sock, state, tmp)
	waitForSocket(t, sock, 5*time.Second)
	id := firstSession(t, d)
	a := dialAttach(t, d, id)
	ctl := dialControl(t, d)

	// Let the startup screen settle and be classified first: whatever
	// Laya makes of it — a wait included — must not keep the next
	// screen from being asked about.
	time.Sleep(3 * time.Second)
	const marker = "Allow command? [y]es / [n]o / [a]lways"
	written := time.Now()
	if err := a.WriteStdin([]byte("clear; printf 'codex wants to run:\\n  rm -rf build/\\n\\n" + marker + " '\n")); err != nil {
		t.Fatal(err)
	}
	var landed time.Duration
	for landed == 0 && time.Since(written) < 8*time.Second {
		if capturedScreen(t, capDir, marker) {
			landed = time.Since(written)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if landed == 0 {
		t.Fatal("the new screen was never classified")
	}
	// 3s is the spec's budget from screen change to reflected state.
	if landed > 3*time.Second {
		t.Errorf("new screen classified after %s, want within 3s", landed)
	}

	if err := ctl.ListSessions(); err != nil {
		t.Fatal(err)
	}
	snap, err := ctl.AwaitSessionsSnapshot(2 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snap.Sessions {
		if s.ID == id {
			t.Logf("classified in %s: state=%q source=%q attention=%v",
				landed.Round(100*time.Millisecond), s.State, s.StateSource, s.NeedsAttention)
			if s.StateSource != wire.StateSourceLaya {
				t.Errorf("state_source = %q, want laya", s.StateSource)
			}
		}
	}
}

// capturedScreen reports whether a private capture of a screen showing
// marker exists — proof the classifier asked about that screen.
func capturedScreen(t *testing.T, dir, marker string) bool {
	t.Helper()
	files, _ := os.ReadDir(dir)
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil || !strings.Contains(string(b), marker) {
			continue
		}
		if info, _ := f.Info(); info.Mode().Perm() != 0o600 {
			t.Errorf("capture %s mode %o, want 600", f.Name(), info.Mode().Perm())
		}
		return true
	}
	return false
}
