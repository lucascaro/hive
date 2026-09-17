package agent

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
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
	if d.SpawnEnv == nil {
		t.Fatal("pi has no SpawnEnv; the todo-tool setting would never reach the extension")
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
	// restart would churn a file Pi may be reading. Compared with
	// os.SameFile rather than ModTime: the write path is temp+rename,
	// so a needless rewrite swaps the inode — which SameFile sees
	// exactly, while a coarse mtime clock can report two distinct files
	// as unchanged.
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
	if !os.SameFile(before, after) {
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
const at = "2026-09-04T12:00:00.000Z";
process.stdout.write(Buffer.concat([
  m.encodeFrames("sess-42", [{ kind: "turn_end", text: "done" }], at),
  m.encodeFrames("sess-42", [
    { kind: "tool_end", tool: "hive_todo", call_id: "c1", ok: false },
    { kind: "plan", items: [{ text: "step", status: "active" }] },
  ], at),
]).toString("base64"));
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

	// The second report: HELLO, then a tool_end and a plan on the same
	// connection — the shape a successful hive_todo call produces.
	if ft, _, err = wire.ReadFrame(r); err != nil || ft != wire.FrameHello {
		t.Fatalf("second report: first frame %s, err %v; want HELLO", ft, err)
	}
	var evs []wire.AgentEvent
	for range 2 {
		ft, payload, err := wire.ReadFrame(r)
		if err != nil || ft != wire.FrameAgentEvent {
			t.Fatalf("second report: frame %s, err %v; want AGENT_EVENT", ft, err)
		}
		var ev wire.AgentEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			t.Fatalf("unmarshal AGENT_EVENT: %v", err)
		}
		if !wire.AgentEventKinds[ev.Kind] {
			t.Errorf("kind = %q, which the daemon's allowlist refuses", ev.Kind)
		}
		evs = append(evs, ev)
	}
	end, plan := evs[0], evs[1]
	if end.Kind != wire.AgentEventToolEnd || end.Tool != "hive_todo" || end.CallID != "c1" {
		t.Errorf("tool_end = %+v", end)
	}
	// ok:false must survive as an explicit false, not decode as absent.
	if end.OK == nil || *end.OK {
		t.Errorf("tool_end ok = %v, want explicit false", end.OK)
	}
	if plan.Kind != wire.AgentEventPlan || len(plan.Items) != 1 ||
		plan.Items[0].Text != "step" || plan.Items[0].Status != wire.PlanStatusActive {
		t.Errorf("plan = %+v", plan)
	}
	if end.At != plan.At {
		t.Errorf("events of one report stamped %q and %q, want the same at", end.At, plan.At)
	}

	if r.Len() != 0 {
		t.Errorf("%d trailing bytes after the two reports", r.Len())
	}
}

// TestPiExtensionKindsAreOnTheAllowlist extracts every kind the
// extension can post and checks it against the daemon's allowlist. The
// kinds are scraped from the source rather than listed here on purpose:
// a hardcoded list only ever proves the kinds someone remembered to add
// to it, so a new post("...") with a kind the ModeEvent arm refuses
// would pass. A refused kind is dropped silently at the daemon, which
// looks exactly like "Pi never reports anything".
//
// Scope: this reads string literals passed to post(...) and every
// `kind: "..."` literal inside a send([...]) call. Every call in the
// extension is written that way on purpose; a kind held in a variable
// would not be seen here, so keep the kind literal at the call site.
func TestPiExtensionKindsAreOnTheAllowlist(t *testing.T) {
	// Comments name every kind in prose, so strip them before scraping.
	var code strings.Builder
	for _, line := range strings.Split(piExtensionSource, "\n") {
		if i := strings.Index(line, "//"); i >= 0 {
			line = line[:i]
		}
		code.WriteString(line)
		code.WriteByte('\n')
	}

	// Every string literal appearing in a post(...) call — which covers
	// post("x") and the ternary in the ui_prompt_start handler. The
	// comparison operands of that ternary (`kind === "confirm"`) are
	// pi's event vocabulary, not Hive's, so equality tests are dropped
	// before the literals are read.
	postCall := regexp.MustCompile(`post\(([^;]*?)\)`)
	comparison := regexp.MustCompile(`[=!]==\s*"[a-z_]+"`)
	literal := regexp.MustCompile(`"([a-z_]+)"`)

	found := map[string]bool{}
	for _, call := range postCall.FindAllStringSubmatch(code.String(), -1) {
		for _, lit := range literal.FindAllStringSubmatch(comparison.ReplaceAllString(call[1], ""), -1) {
			found[lit[1]] = true
		}
	}
	// Batched reports: send([{ kind: "tool_end", ... }, { kind: "plan", ... }]).
	// Scoped to send calls because runEnd's own { kind: "done" } result is
	// not a wire kind.
	sendCall := regexp.MustCompile(`(?s)send\(\[(.*?)\]\)`)
	kindField := regexp.MustCompile(`kind:\s*"([a-z_]+)"`)
	for _, call := range sendCall.FindAllStringSubmatch(code.String(), -1) {
		for _, lit := range kindField.FindAllStringSubmatch(call[1], -1) {
			found[lit[1]] = true
		}
	}
	if len(found) == 0 {
		t.Fatal("scraped no kinds from the extension; the regex no longer matches post(...)")
	}
	for kind := range found {
		if !wire.AgentEventKinds[kind] {
			t.Errorf("extension posts %q, which wire.AgentEventKinds refuses", kind)
		}
	}

	// And the kinds the tier is built on are all still reported, so a
	// silently deleted handler fails here too.
	for _, kind := range []string{
		"ping", "prompt", "permission_resolved", "turn_end", "idle", "error",
		"waiting_permission", "waiting_input", "session_end",
		"tool_start", "tool_end", "plan",
	} {
		if !found[kind] {
			t.Errorf("extension no longer posts %q", kind)
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
