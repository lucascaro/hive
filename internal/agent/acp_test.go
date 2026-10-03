package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func stubLoginPATH(t *testing.T, path string) {
	t.Helper()
	prev := loginPATHFn
	loginPATHFn = func() string { return path }
	t.Cleanup(func() { loginPATHFn = prev })
}

func fakeBins(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if runtime.GOOS == "windows" {
			n += ".exe" // LookPathIn honours PATHEXT there, not mode bits
		}
		if err := os.WriteFile(filepath.Join(dir, n), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// The adapter versions are what spike 492 probed. A change here must
// come with a re-run of scripts/acp-probe, so the pin is asserted.
func TestACPSpecsPinned(t *testing.T) {
	want := map[ID][]string{
		IDClaude: {"npx", "-y", "@agentclientprotocol/claude-agent-acp@0.85.1"},
		IDCodex:  {"npx", "-y", "@agentclientprotocol/codex-acp@2.1.1"},
		IDPi:     {"npx", "-y", "pi-acp@0.0.34"},
	}
	for id, argv := range want {
		d, _ := Get(id)
		if got := d.ACP(); got == nil || strings.Join(got.Argv, " ") != strings.Join(argv, " ") || got.Experimental {
			t.Errorf("%s ACP = %+v, want %v, not experimental", id, got, argv)
		}
	}
	for _, id := range []ID{IDGemini, IDCopilot} {
		d, _ := Get(id)
		if got := d.ACP(); got == nil || !got.Experimental {
			t.Errorf("%s ACP = %+v, want an experimental spec", id, got)
		}
	}
	for _, id := range []ID{IDShell, IDAider} {
		d, _ := Get(id)
		if d.ACP() != nil {
			t.Errorf("%s has an ACP spec, want none", id)
		}
	}
}

func TestACPAvailableNoNodeReason(t *testing.T) {
	stubLoginPATH(t, fakeBins(t, "claude"))
	d, _ := Get(IDClaude)
	ok, reason := d.ACPAvailable()
	if ok || !strings.Contains(reason, "Node.js") {
		t.Errorf("ACPAvailable without npx = %v, %q; want false naming Node.js", ok, reason)
	}
}

func TestACPAvailableNeedsAgentCLI(t *testing.T) {
	stubLoginPATH(t, fakeBins(t, "npx"))
	d, _ := Get(IDCodex)
	if ok, reason := d.ACPAvailable(); ok || !strings.Contains(reason, "codex") {
		t.Errorf("ACPAvailable without codex = %v, %q; want false naming codex", ok, reason)
	}
	stubLoginPATH(t, fakeBins(t, "npx", "codex"))
	if ok, reason := d.ACPAvailable(); !ok {
		t.Errorf("ACPAvailable with npx and codex = false (%q), want true", reason)
	}
}

func TestACPAvailableUnsupportedAgent(t *testing.T) {
	d, _ := Get(IDAider)
	if ok, reason := d.ACPAvailable(); ok || reason == "" {
		t.Errorf("Aider ACPAvailable = %v, %q; want false with a reason", ok, reason)
	}
}
