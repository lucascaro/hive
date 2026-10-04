package agent

import (
	"os"
	"os/exec"
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
	ok, reason := d.ACPAvailable(DefaultSettings())
	if ok || !strings.Contains(reason, "Node.js") {
		t.Errorf("ACPAvailable without npx = %v, %q; want false naming Node.js", ok, reason)
	}
}

func TestACPAvailableNeedsAgentCLI(t *testing.T) {
	stubLoginPATH(t, fakeBins(t, "npx"))
	d, _ := Get(IDCodex)
	if ok, reason := d.ACPAvailable(DefaultSettings()); ok || !strings.Contains(reason, "codex") {
		t.Errorf("ACPAvailable without codex = %v, %q; want false naming codex", ok, reason)
	}
	stubLoginPATH(t, fakeBins(t, "npx", "codex"))
	if ok, reason := d.ACPAvailable(DefaultSettings()); !ok {
		t.Errorf("ACPAvailable with npx and codex = false (%q), want true", reason)
	}
}

func TestACPAvailableUnsupportedAgent(t *testing.T) {
	d, _ := Get(IDAider)
	if ok, reason := d.ACPAvailable(DefaultSettings()); ok || reason == "" {
		t.Errorf("Aider ACPAvailable = %v, %q; want false with a reason", ok, reason)
	}
}

// Criterion 5: with no setting, each agent runs at its most restrictive
// working mode; Codex's own default ("agent", which called the submit
// tool unasked in spike 492, F4) is never the ceiling.
func TestCeilingDefaultsMostRestrictive(t *testing.T) {
	st := DefaultSettings()
	for id, want := range map[ID]string{IDClaude: "default", IDCodex: "read-only", IDPi: ACPModeOff, IDGemini: "", IDAider: ""} {
		if got := st.ACPCeiling(id); got != want {
			t.Errorf("ACPCeiling(%s) = %q, want %q", id, got, want)
		}
	}
	codex := acpSpecs[IDCodex]
	if codex.ModeRank("read-only") != 0 || codex.ModeRank("agent") <= codex.ModeRank("workspace-write") {
		t.Errorf("Codex modes out of order: %+v", codex.Modes)
	}
	claude := acpSpecs[IDClaude]
	if claude.ModeRank("bypassPermissions") != len(claude.Modes)-1 || claude.ModeRank("default") >= claude.ModeRank("acceptEdits") {
		t.Errorf("Claude modes out of order: %+v", claude.Modes)
	}
}

func TestCeilingSettingAndUnknownFallsBack(t *testing.T) {
	st := DefaultSettings()
	st.ACPModeCeiling = map[string]string{"claude": "acceptEdits", "codex": "no-such-mode"}
	if got := st.ACPCeiling(IDClaude); got != "acceptEdits" {
		t.Errorf("ACPCeiling(claude) = %q, want the setting acceptEdits", got)
	}
	if got := st.ACPCeiling(IDCodex); got != "read-only" {
		t.Errorf("ACPCeiling(codex) with an unknown mode = %q, want the default read-only", got)
	}
}

func TestPiRefusedWithoutUnattended(t *testing.T) {
	stubLoginPATH(t, fakeBins(t, "npx", "pi"))
	d, _ := Get(IDPi)
	if ok, reason := d.ACPAvailable(DefaultSettings()); ok || !strings.Contains(reason, "unattended") {
		t.Errorf("Pi ACPAvailable by default = %v, %q; want false naming unattended", ok, reason)
	}
	st := DefaultSettings()
	st.ACPModeCeiling = map[string]string{"pi": ACPModeUnattended}
	if ok, reason := d.ACPAvailable(st); !ok {
		t.Errorf("Pi ACPAvailable with unattended = false (%q), want true", reason)
	}
}

// Gemini and Copilot were never probed: Hive does not know their modes,
// so it cannot cap them, and ACP stays off with a reason.
func TestUnprobedAgentRefusedWithReason(t *testing.T) {
	stubLoginPATH(t, fakeBins(t, "gemini", "copilot"))
	for _, id := range []ID{IDGemini, IDCopilot} {
		d, _ := Get(id)
		if ok, reason := d.ACPAvailable(DefaultSettings()); ok || !strings.Contains(reason, "permission mode") {
			t.Errorf("%s ACPAvailable = %v, %q; want false naming the permission mode", id, ok, reason)
		}
	}
}

// The shim is what makes an ACP Pi load Hive's extension: pi-acp runs
// it in place of pi, with pi's fixed arguments.
func TestPiACPShimRunsPiWithExtension(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the .cmd shim needs cmd.exe")
	}
	state := filepath.Join(t.TempDir(), "it's state") // a quote in the path
	if _, err := PiACPShim(state); err == nil {
		t.Fatal("PiACPShim with no extension on disk succeeded; a shim loading nothing would hide that")
	}
	if err := EnsurePiExtension(state); err != nil {
		t.Fatal(err)
	}
	shim, err := PiACPShim(state)
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	out := filepath.Join(t.TempDir(), "args")
	if err := os.WriteFile(filepath.Join(bin, "pi"), []byte("#!/bin/sh\nprintf '%s\\n' \"$@\" > \""+out+"\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(shim, "--mode", "rpc", "--no-themes")
	cmd.Env = append(os.Environ(), "PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("shim: %v\n%s", err, b)
	}
	got, _ := os.ReadFile(out)
	want := "-e\n" + filepath.Join(state, PiExtensionRelPath) + "\n--mode\nrpc\n--no-themes\n"
	if string(got) != want {
		t.Errorf("pi got args\n%q\nwant\n%q", got, want)
	}
}
