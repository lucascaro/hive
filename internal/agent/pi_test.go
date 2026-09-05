package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

func TestPiDefUsesSpawnArgs(t *testing.T) {
	d, ok := Get(IDPi)
	if !ok {
		t.Fatal("pi missing from the catalog")
	}
	if d.SpawnArgs == nil {
		t.Fatal("pi has no SpawnArgs; the extension tier would never be wired")
	}
}

func TestEnsurePiExtensionWritesAtomicallyAndOnlyWhenStale(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, PiExtensionRelPath)

	if err := EnsurePiExtension(dir); err != nil {
		t.Fatalf("EnsurePiExtension: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read written extension: %v", err)
	}
	if string(got) != piExtensionSource {
		t.Fatal("written extension differs from the embedded source")
	}

	// Same content: the file must not be touched, or every daemon
	// restart would churn a file Pi may be reading.
	before, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if err := EnsurePiExtension(dir); err != nil {
		t.Fatalf("second EnsurePiExtension: %v", err)
	}
	after, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("EnsurePiExtension rewrote an already-current extension")
	}

	// Stale content (an older daemon's copy) must be replaced.
	if err := os.WriteFile(dst, []byte("// stale\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := EnsurePiExtension(dir); err != nil {
		t.Fatalf("third EnsurePiExtension: %v", err)
	}
	got, _ = os.ReadFile(dst)
	if string(got) != piExtensionSource {
		t.Error("EnsurePiExtension left a stale extension in place")
	}

	// No temp files left behind by any of the three passes.
	entries, err := os.ReadDir(filepath.Dir(dst))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".hive-") {
			t.Errorf("leftover temp file %s", e.Name())
		}
	}
}

func TestEnsurePiExtensionIgnoresEmptyStateDir(t *testing.T) {
	if err := EnsurePiExtension(""); err != nil {
		t.Fatalf("EnsurePiExtension(\"\") = %v, want nil", err)
	}
}

func TestPiSpawnArgs(t *testing.T) {
	dir := t.TempDir()

	// No extension on disk: heuristic tier, never a broken `-e`.
	if got := piSpawnArgs(SpawnInfo{StateDir: dir}); got != nil {
		t.Errorf("piSpawnArgs with no extension = %v, want nil", got)
	}
	if got := piSpawnArgs(SpawnInfo{}); got != nil {
		t.Errorf("piSpawnArgs with no state dir = %v, want nil", got)
	}

	if err := EnsurePiExtension(dir); err != nil {
		t.Fatal(err)
	}
	want := []string{"-e", filepath.Join(dir, PiExtensionRelPath)}
	got := piSpawnArgs(SpawnInfo{StateDir: dir})
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("piSpawnArgs = %v, want %v", got, want)
	}
}

// nodeForTS returns the path to a node that can load a .ts file
// directly, or "" when there is none. It probes the real behaviour
// rather than parsing a version string: node has moved type stripping
// twice (behind --experimental-strip-types in 22.6, on by default in
// 23.6), so "which node" and "which flags" are both wrong questions to
// hard-code in a test.
//
// The distinction matters because LookPath("node") succeeding does not
// mean the node it found can run these tests — a contributor on an
// older node must get a skip, not a red suite. CI pins node 24
// (.github/workflows/ci.yml), so these tests do run there.
func nodeForTS(t *testing.T) string {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		return ""
	}
	probe := filepath.Join(t.TempDir(), "probe.ts")
	if err := os.WriteFile(probe, []byte("export const n: number = 1;\n"), 0o644); err != nil {
		t.Fatalf("write ts probe: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, node, probe).Run(); err != nil {
		return ""
	}
	return node
}

// TestPiExtensionFramesAreValidWireFrames is the cross-language
// contract check: the extension hand-rolls Hive's frame header in
// TypeScript, so the only thing that proves it stays in sync with
// internal/wire is decoding its real output with the real reader.
// Skipped when node is unavailable (it is present in CI, which runs the
// frontend suites).
func TestPiExtensionFramesAreValidWireFrames(t *testing.T) {
	node := nodeForTS(t)
	if node == "" {
		t.Skip("no node that can load .ts (needs node >= 23.6, or 22.6 with --experimental-strip-types)")
	}
	script := `
const m = await import("./pi/hive.ts");
process.stdout.write(m.encodeFrames("sess-42", "turn_end", "done", "2026-09-04T12:00:00.000Z").toString("base64"));
`
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--input-type=module", "-e", script)
	cmd.Dir = "." // internal/agent
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("run node: %v\n%s", err, stderr.String())
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
	if err != nil {
		t.Fatalf("decode node output: %v", err)
	}

	r := bytes.NewReader(raw)

	ft, payload, err := wire.ReadFrame(r)
	if err != nil {
		t.Fatalf("read HELLO: %v", err)
	}
	if ft != wire.FrameHello {
		t.Fatalf("first frame is %s, want HELLO", ft)
	}
	var hello wire.Hello
	if err := json.Unmarshal(payload, &hello); err != nil {
		t.Fatalf("unmarshal HELLO: %v", err)
	}
	if hello.Version != wire.PROTOCOL_VERSION {
		t.Errorf("HELLO version = %d, want %d", hello.Version, wire.PROTOCOL_VERSION)
	}
	if hello.Mode != wire.ModeEvent {
		t.Errorf("HELLO mode = %q, want %q", hello.Mode, wire.ModeEvent)
	}

	ft, payload, err = wire.ReadFrame(r)
	if err != nil {
		t.Fatalf("read AGENT_EVENT: %v", err)
	}
	if ft != wire.FrameAgentEvent {
		t.Fatalf("second frame is %s, want AGENT_EVENT", ft)
	}
	var ev wire.AgentEvent
	if err := json.Unmarshal(payload, &ev); err != nil {
		t.Fatalf("unmarshal AGENT_EVENT: %v", err)
	}
	if ev.SessionID != "sess-42" {
		t.Errorf("session_id = %q, want %q", ev.SessionID, "sess-42")
	}
	if ev.Source != wire.StateSourceExtension {
		t.Errorf("source = %q, want %q — the daemon refuses anything else", ev.Source, wire.StateSourceExtension)
	}
	if !wire.AgentEventKinds[ev.Kind] {
		t.Errorf("kind = %q, which the daemon's allowlist refuses", ev.Kind)
	}
	if ev.Text != "done" || ev.At == "" {
		t.Errorf("text/at = %q/%q, want %q/non-empty", ev.Text, ev.At, "done")
	}

	if r.Len() != 0 {
		t.Errorf("%d trailing bytes after the two frames", r.Len())
	}
}

// TestPiExtensionKindsAreOnTheAllowlist pins every kind string the
// extension can post against the daemon's allowlist: a typo there is
// silently dropped at the ModeEvent arm, which looks exactly like "Pi
// never reports anything".
func TestPiExtensionKindsAreOnTheAllowlist(t *testing.T) {
	// Comments in the extension mention every kind by name, so scanning
	// the raw source would keep passing for a kind that survives only in
	// prose. Strip line comments first.
	var code strings.Builder
	for _, line := range strings.Split(piExtensionSource, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}
	src := code.String()

	for _, kind := range []string{
		"ping", "prompt", "permission_resolved", "turn_end",
		"waiting_permission", "waiting_input", "session_end",
	} {
		if !strings.Contains(src, `"`+kind+`"`) {
			t.Errorf("extension no longer posts %q; update this test or the extension", kind)
		}
		if !wire.AgentEventKinds[kind] {
			t.Errorf("extension posts %q, which wire.AgentEventKinds refuses", kind)
		}
	}
}

// TestPiExtensionRunsNodeTests runs the extension's own TS suite (the
// inert-outside-Hive guard, the event subscriptions, truncation, and
// the session-format walk) so `scripts/test.sh go` covers it too.
func TestPiExtensionRunsNodeTests(t *testing.T) {
	node := nodeForTS(t)
	if node == "" {
		t.Skip("no node that can load .ts (needs node >= 23.6, or 22.6 with --experimental-strip-types)")
	}
	// Bounded: this drives a real subprocess that opens sockets, and a
	// node that never exits would otherwise hang the whole package
	// until the go test deadline — turning one stuck test into a red
	// suite with no indication of which test was at fault.
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, node, "--test", filepath.Join("pi", "hive.test.ts"))
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		t.Fatalf("node --test pi/hive.test.ts did not exit within 90s\n%s", out)
	}
	if err != nil {
		t.Fatalf("node --test pi/hive.test.ts: %v\n%s", err, out)
	}
}
