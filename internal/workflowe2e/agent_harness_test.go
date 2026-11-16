// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package workflowe2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp"
	"github.com/lucascaro/hive/internal/acp/acptest"
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// TestMain re-executes this test binary as the fake ACP adapter, and gives
// agent availability a login shell that finds claude and npx: the daemon asks
// the user's login shell for its PATH, which a CI machine does not have an
// agent on.
func TestMain(m *testing.M) {
	if acptest.IsAgent() {
		acptest.Main()
		return
	}
	var cleanup func()
	if runtime.GOOS != "windows" {
		cleanup = fakeLoginShell()
	}
	code := m.Run()
	if cleanup != nil {
		cleanup()
	}
	os.Exit(code)
}

// fakeLoginShell points $SHELL at a script that runs the daemon's PATH probe
// with a directory of stub claude and npx binaries in front of the real PATH.
func fakeLoginShell() (cleanup func()) {
	dir, err := os.MkdirTemp("", "wfe2e-shell")
	if err != nil {
		panic(err)
	}
	bin := filepath.Join(dir, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		panic(err)
	}
	for _, name := range []string{"claude", "npx"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			panic(err)
		}
	}
	shell := filepath.Join(dir, "shell")
	script := "#!/bin/sh\nfor last; do :; done\nPATH=\"" + bin + ":$PATH\" exec /bin/sh -c \"$last\"\n"
	if err := os.WriteFile(shell, []byte(script), 0o755); err != nil {
		panic(err)
	}
	_ = os.Setenv("SHELL", shell)
	return func() { _ = os.RemoveAll(dir) }
}

// newAgentFixture is newFixture with ACP sessions routed to the fake adapter,
// whose store (and turn gates) live in the returned directory. The routing is
// process-wide, so these tests never run in parallel.
func newAgentFixture(t *testing.T, flags ...string) (*fixture, string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the workflow engine e2e needs POSIX process groups and git config isolation")
	}
	dir := t.TempDir()
	// Registered first, so it is undone after the daemon has stopped.
	restore := registry.SetACPCommandForTest(func(agent.Def) ([]string, []string) {
		return []string{os.Args[0]}, append(acp.AdapterEnv(os.Environ(), ""), acptest.Env(dir, flags...)...)
	})
	t.Cleanup(restore)
	return newFixture(t), dir
}

// ---------------------------------------------------------------- turn gates

var turnFile = regexp.MustCompile(`^turn-(.+)-(\d+)\.waiting$`)

// turn is one gated agent turn: the fake adapter has received the prompt and
// waits for the test to let it end. The test plays the agent: it writes files
// in the session's worktree and submits the result the way its submit server
// would.
type turn struct {
	t    *testing.T
	f    *fixture
	sid  string // the adapter's id for the session, as the fake names its gate files
	n    int
	hold bool
}

// id is Hive's id for the session this turn belongs to: the registry knows the adapter's id as the entry's AgentSessionID.
func (tc *turn) id() string {
	reg := tc.f.d.Registry()
	for _, s := range reg.List() {
		if e := reg.Get(s.ID); e != nil && e.AgentSessionID == tc.sid {
			return s.ID
		}
	}
	tc.t.Errorf("no session has adapter id %s", tc.sid)
	return ""
}

// Hold keeps the turn open after the handler returns; Release ends it later.
func (tc *turn) Hold() { tc.hold = true }

func (tc *turn) Release(dir string) {
	_ = os.WriteFile(filepath.Join(dir, "turn-"+tc.sid+"-"+itoa(tc.n)+".go"), nil, 0o600)
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func (tc *turn) info() wire.SessionInfo {
	id := tc.id()
	for _, s := range tc.f.d.Registry().List() {
		if s.ID == id {
			return s
		}
	}
	return wire.SessionInfo{}
}

// node is the key of the workflow node this session runs: its name is "<workflow> · <key>".
func (tc *turn) node() string {
	_, key, _ := strings.Cut(tc.info().Name, " · ")
	return key
}

func (tc *turn) worktree() string { return tc.info().WorktreePath }

func (tc *turn) write(rel, content string) {
	p := filepath.Join(tc.worktree(), rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		tc.t.Errorf("mkdir: %v", err)
		return
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		tc.t.Errorf("write %s: %v", rel, err)
	}
}

func (tc *turn) exists(rel string) bool {
	_, err := os.Stat(filepath.Join(tc.worktree(), rel))
	return err == nil
}

// submit sends the result of the running turn, as the session's submit server would.
func (tc *turn) submit(data map[string]any) {
	tc.submitEnvelope(map[string]any{"status": "ok", "data": data})
}

func (tc *turn) submitEnvelope(env map[string]any) {
	b, _ := json.Marshal(env)
	reg, id := tc.f.d.Registry(), tc.id()
	if err := reg.SubmitResult(id, reg.ACPNonceForTest(id), b); err != nil {
		tc.t.Errorf("SubmitResult(%s): %v", id, err)
	}
}

// onTurns runs fn for every gated turn, one at a time, in the order they
// start, and lets the turn end when fn returns (unless it held it).
func (f *fixture) onTurns(dir string, fn func(tc *turn)) {
	f.t.Helper()
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		seen := map[string]bool{}
		for {
			select {
			case <-stop:
				return
			case <-time.After(10 * time.Millisecond):
			}
			entries, _ := os.ReadDir(dir)
			for _, e := range entries {
				m := turnFile.FindStringSubmatch(e.Name())
				if m == nil || seen[e.Name()] {
					continue
				}
				seen[e.Name()] = true
				n := 0
				for _, c := range m[2] {
					n = n*10 + int(c-'0')
				}
				tc := &turn{t: f.t, f: f, sid: m[1], n: n}
				fn(tc)
				if !tc.hold {
					tc.Release(dir)
				}
			}
		}
	}()
	f.t.Cleanup(func() {
		close(stop)
		wg.Wait()
	})
}

// ------------------------------------------------------------------ IR shapes

func object(props map[string]any, required ...string) map[string]any {
	req := make([]any, len(required))
	for i, r := range required {
		req[i] = r
	}
	return map[string]any{"type": "object", "properties": props, "required": req, "additionalProperties": false}
}

var (
	str        = map[string]any{"type": "string"}
	summaryOut = object(map[string]any{"summary": str}, "summary")
	verdictOut = object(map[string]any{"verdict": map[string]any{"type": "string", "enum": []any{"approve", "revise"}}, "feedback": str}, "verdict", "feedback")
	tasksOut   = object(map[string]any{"tasks": map[string]any{"type": "array", "items": str}}, "tasks")
)

// agentNode is an agent node; extra overrides fields (mode, worktree, timeout_s, output).
func agentNode(id, prompt string, extra map[string]any) map[string]any {
	n := map[string]any{"id": id, "kind": "agent", "agent": "claude", "worktree": "own", "timeout_s": 120, "prompt": prompt, "output": summaryOut}
	for k, v := range extra {
		n[k] = v
	}
	return n
}

func sameTree(of string) map[string]any { return map[string]any{"worktree": map[string]any{"of": of}} }

// ---------------------------------------------------------------- run helpers

// nodeInfo is node.get for a node.
func (f *fixture) nodeInfo(runID, key string) map[string]any {
	f.t.Helper()
	return f.ok("node.get", map[string]any{"run_id": runID, "key": key})
}

// nodeOutput is a done node's output, decoded.
func (f *fixture) nodeOutput(runID, key string) map[string]any {
	f.t.Helper()
	var out map[string]any
	s, _ := f.nodeInfo(runID, key)["output"].(string)
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		f.t.Fatalf("node %s output %q: %v", key, s, err)
	}
	return out
}

// eventTypes lists a node's event types in order, from run.events.
func (f *fixture) eventTypes(runID, key string) []string {
	f.t.Helper()
	evs, _ := f.ok("run.events", map[string]any{"run_id": runID, "after_seq": 0, "key": key})["events"].([]any)
	out := make([]string, 0, len(evs))
	for _, e := range evs {
		out = append(out, e.(map[string]any)["type"].(string))
	}
	return out
}

func (f *fixture) runError(runID string) string {
	f.t.Helper()
	run, _ := f.ok("run.get", map[string]any{"run_id": runID})["run"].(map[string]any)
	s, _ := run["error"].(string)
	return s
}

// branches lists the run's branches in the repo, newest listing first as git sorts them.
func branches(t *testing.T, repo, runID string) []string {
	t.Helper()
	out := git(t, repo, "for-each-ref", "--format=%(refname:short)", "refs/heads/hive/wf/"+runID+"/")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// treeHas reports whether branch holds path.
func treeHas(t *testing.T, repo, branch, path string) bool {
	t.Helper()
	return git(t, repo, "ls-tree", "--name-only", branch, "--", path) != ""
}

// fakeCalls are the fake adapter's calls.log lines starting with prefix.
func fakeCalls(dir, prefix string) []string {
	var out []string
	for _, l := range acptest.Calls(dir) {
		if strings.HasPrefix(l, prefix) {
			out = append(out, l)
		}
	}
	return out
}
