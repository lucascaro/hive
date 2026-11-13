// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Package workflowe2e runs the workflow engine plugin (plugins/workflows)
// under a real, in-process hived: the real plugin Manager starts the real
// plugin, a test client plays the GUI over PLUGIN_MESSAGE, and the daemon can
// be stopped and started again on the same state directory.
//
// Isolation is mandatory (spec 529, Conventions): the harness gives the
// daemon temp HIVE_SOCKET and HIVE_STATE_DIR paths and testclient's
// RequireIsolation refuses to go on if they are not temp.
package workflowe2e

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/wire/testclient"
)

const pluginID = "workflows"

// requireNode finds node and decides what to do about its version. The
// plugin's manifest needs Node 26. On a machine with an older Node the
// harness installs a copy whose manifest drops that floor so the engine can
// still be exercised (type stripping and node:sqlite exist from Node 24);
// where HIVE_REQUIRE_NODE=26 is set (CI's Node 26 leg) a missing or older Node
// fails instead, so the leg can never go green by skipping.
func requireNode(t *testing.T) (nodeBin string, patchFloor bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the workflow engine e2e needs POSIX process groups and git config isolation")
	}
	want := os.Getenv("HIVE_REQUIRE_NODE")
	bin, err := exec.LookPath("node")
	if err != nil {
		if want != "" {
			t.Fatalf("HIVE_REQUIRE_NODE=%s but node is not installed", want)
		}
		t.Skip("node not installed")
	}
	out, err := exec.Command(bin, "--version").Output()
	if err != nil {
		t.Fatalf("node --version: %v", err)
	}
	major, _ := strconv.Atoi(strings.SplitN(strings.TrimPrefix(strings.TrimSpace(string(out)), "v"), ".", 2)[0])
	if major >= 26 {
		return bin, false
	}
	if want != "" {
		t.Fatalf("HIVE_REQUIRE_NODE=%s but node is %s", want, strings.TrimSpace(string(out)))
	}
	if major < 24 {
		t.Skipf("node %s is too old for type stripping and node:sqlite", strings.TrimSpace(string(out)))
	}
	t.Logf("node %s: installing the plugin with its Node 26 floor removed (CI runs the unmodified manifest)", strings.TrimSpace(string(out)))
	return bin, true
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for d := dir; ; d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		if d == filepath.Dir(d) {
			t.Fatal("go.mod not found")
		}
	}
}

// pluginCopy copies plugins/workflows (without its tests) for installation,
// optionally with the Node floor dropped from the manifest.
func pluginCopy(t *testing.T, patchFloor bool) string {
	t.Helper()
	src := filepath.Join(repoRoot(t), "plugins", "workflows")
	dst := t.TempDir()
	err := filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if rel == "test" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dst, rel), 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if rel == "hive-plugin.json" && patchFloor {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				return err
			}
			delete(m, "node")
			if b, err = json.Marshal(m); err != nil {
				return err
			}
		}
		return os.WriteFile(filepath.Join(dst, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

type fixture struct {
	t        *testing.T
	sockDir  string
	stateDir string
	d        *daemon.Daemon
	cancel   context.CancelFunc
	runDone  chan struct{}
	c        *testclient.Client
	seq      int
	ready    int // "workflows: ready" lines seen so far
	node     string
}

// newFixture starts a daemon, installs and enables the plugin, and waits for
// it to be ready. The test seam is on: a run starts from an IR until
// run.start arrives (B4).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	node, patch := requireNode(t)
	sockDir, err := os.MkdirTemp("/tmp", "wfe2e")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(sockDir) })
	f := &fixture{t: t, sockDir: sockDir, stateDir: filepath.Join(sockDir, "state"), node: node}
	t.Setenv("HIVE_SOCKET", filepath.Join(sockDir, "s"))
	t.Setenv("HIVE_STATE_DIR", f.stateDir)
	t.Setenv("HIVE_WF_TEST_SEAM", "1")
	// The test machine's git config must not bring identity, hooks or signing into the daemon's git calls.
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(sockDir, "gitconfig"))
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	if err := testclient.RequireIsolation(); err != nil {
		t.Fatal(err)
	}
	f.start()
	t.Cleanup(f.stop)

	if err := f.c.InstallPlugin(pluginCopy(t, patch), "n-install"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.c.AwaitPluginEvent(func(ev wire.PluginEvent) bool { return ev.Kind == wire.PluginEventAdded && ev.Plugin.ID == pluginID }, 10*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := f.c.SetPluginEnabled(pluginID, true); err != nil {
		t.Fatal(err)
	}
	f.waitReady()
	return f
}

// start brings up a daemon on the fixture's state dir and a UI-capable client.
func (f *fixture) start() {
	t := f.t
	t.Helper()
	d, err := daemon.New(daemon.Config{SocketPath: filepath.Join(f.sockDir, "s"), StateDir: f.stateDir})
	if err != nil {
		t.Fatalf("daemon.New: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f.d, f.cancel, f.runDone = d, cancel, make(chan struct{})
	go func() {
		defer close(f.runDone)
		_ = d.Run(ctx)
	}()
	dctx, dcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer dcancel()
	c, err := testclient.Dial(dctx, filepath.Join(f.sockDir, "s"))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := c.Handshake(wire.Hello{Mode: wire.ModeControl, Client: "workflowe2e/gui"}); err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if err := c.DrainInitialSnapshots(5 * time.Second); err != nil {
		t.Fatalf("drain: %v", err)
	}
	// A client that shows the plugin's UI is what receives its PLUGIN_MESSAGEs.
	if err := c.SetClientUI([]string{pluginID}); err != nil {
		t.Fatal(err)
	}
	f.c = c
}

// stop shuts the daemon down the way a normal stop does: the plugin gets
// SIGTERM, then KILL.
func (f *fixture) stop() {
	if f.d == nil {
		return
	}
	_ = f.c.Close()
	f.cancel()
	_ = f.d.Close()
	<-f.runDone
	f.d = nil
}

// restart is a hived restart on the same state directory: the plugin
// comes back on its own, because it is still enabled.
func (f *fixture) restart() {
	f.t.Helper()
	f.stop()
	f.start()
	f.waitReady()
}

func (f *fixture) logPath() string {
	return filepath.Join(f.stateDir, "plugin-data", pluginID, "plugin.log")
}

// waitReady waits for one more "workflows: ready" line in the plugin's log: the plugin is connected.
func (f *fixture) waitReady() {
	f.t.Helper()
	f.ready++
	f.eventually(60*time.Second, fmt.Sprintf("the plugin to log ready (#%d)", f.ready), func() bool {
		b, _ := os.ReadFile(f.logPath())
		return strings.Count(string(b), "workflows: ready") >= f.ready
	})
}

func (f *fixture) eventually(d time.Duration, what string, cond func() bool) {
	f.t.Helper()
	deadline := time.Now().Add(d)
	for !cond() {
		if time.Now().After(deadline) {
			b, _ := os.ReadFile(f.logPath())
			f.t.Fatalf("timed out waiting for %s; plugin log:\n%s", what, b)
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// rpc sends a topic request as the GUI and waits for its reply. Pushes
// (run.event) that arrive meanwhile are dropped: these tests read state through topics.
func (f *fixture) rpc(topic string, data map[string]any) map[string]any {
	f.t.Helper()
	f.seq++
	nonce := fmt.Sprintf("n%d", f.seq)
	body := map[string]any{"nonce": nonce}
	for k, v := range data {
		body[k] = v
	}
	b, err := json.Marshal(body)
	if err != nil {
		f.t.Fatal(err)
	}
	if err := f.c.SendPluginMessage(pluginID, topic, b); err != nil {
		f.t.Fatal(err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		m, err := f.c.AwaitPluginMessage(time.Until(deadline))
		if err != nil {
			f.t.Fatalf("%s: %v", topic, err)
		}
		if m.PluginID != pluginID || m.Topic != topic+".reply" {
			continue
		}
		var out map[string]any
		if err := json.Unmarshal(m.Data, &out); err != nil {
			f.t.Fatal(err)
		}
		if out["nonce"] == nonce {
			return out
		}
	}
	f.t.Fatalf("no reply to %s", topic)
	return nil
}

// ok is rpc, failing the test unless the reply says ok.
func (f *fixture) ok(topic string, data map[string]any) map[string]any {
	f.t.Helper()
	out := f.rpc(topic, data)
	if out["ok"] != true {
		f.t.Fatalf("%s: %v", topic, out["error"])
	}
	return out
}

func (f *fixture) runState(runID string) string {
	f.t.Helper()
	run, _ := f.ok("run.get", map[string]any{"run_id": runID})["run"].(map[string]any)
	s, _ := run["state"].(string)
	return s
}

// nodeState is a node's state and attempt from run.nodes.
func (f *fixture) nodeState(runID, key string) (string, int) {
	f.t.Helper()
	nodes, _ := f.ok("run.nodes", map[string]any{"run_id": runID})["nodes"].([]any)
	for _, n := range nodes {
		m := n.(map[string]any)
		if m["key"] == key {
			a, _ := m["attempt"].(float64)
			return m["state"].(string), int(a)
		}
	}
	return "", 0
}

func (f *fixture) waitNode(runID, key, state string, d time.Duration) {
	f.t.Helper()
	f.eventually(d, fmt.Sprintf("node %s to be %s (%s)", key, state, f.describe(runID, key)), func() bool {
		s, _ := f.nodeState(runID, key)
		return s == state
	})
}

// describe is what a failed wait prints: the node and the run, as the engine reports them.
func (f *fixture) describe(runID, key string) string {
	node, _ := json.Marshal(f.rpc("node.get", map[string]any{"run_id": runID, "key": key}))
	run, _ := json.Marshal(f.rpc("run.get", map[string]any{"run_id": runID})["run"])
	return fmt.Sprintf("node %s; run %s", node, run)
}

func (f *fixture) waitRun(runID, state string, d time.Duration) {
	f.t.Helper()
	f.eventually(d, fmt.Sprintf("run %s to be %s", runID, state), func() bool { return f.runState(runID) == state })
}

// startRun starts a run from an IR through the test seam.
func (f *fixture) startRun(projectID string, ir map[string]any) string {
	f.t.Helper()
	out := f.ok("test.start-run", map[string]any{"project_id": projectID, "ir": ir, "input": map[string]any{}})
	return out["run_id"].(string)
}

// project makes a git repo with one commit and a hived project on it.
func (f *fixture) project() (projectID, repo string) {
	t := f.t
	t.Helper()
	repo = t.TempDir()
	git(t, repo, "init", "-q", "-b", "main")
	git(t, repo, "config", "user.name", "Test User")
	git(t, repo, "config", "user.email", "test@example.com")
	git(t, repo, "commit", "--allow-empty", "-q", "-m", "one")
	p, err := f.d.Registry().CreateProject(wire.CreateProjectReq{Name: "wf", Cwd: repo})
	if err != nil {
		t.Fatal(err)
	}
	return p.ID, repo
}

func git(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// ---- IR builders: the JSON the plugin stores, no TypeScript needed here.

var emptyInput = map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}, "additionalProperties": false}

func ir(name string, nodes []any, edges ...map[string]any) map[string]any {
	es := make([]any, len(edges))
	for i, e := range edges {
		es[i] = e
	}
	return map[string]any{"ir": 1, "name": name, "input": emptyInput, "nodes": nodes, "edges": es}
}

func check(id string, argv ...string) map[string]any {
	return map[string]any{"id": id, "kind": "check", "argv": argv, "timeout_s": 120}
}

func human(id, prompt string) map[string]any {
	return map[string]any{
		"id": id, "kind": "human", "prompt": prompt,
		"output": map[string]any{"type": "object", "properties": map[string]any{"yes": map[string]any{"type": "boolean"}}, "required": []any{"yes"}, "additionalProperties": false},
	}
}

func edge(from, to string) map[string]any { return map[string]any{"from": from, "to": to} }

// nodeScript is a `node -e` check argv.
func (f *fixture) nodeScript(script string) []string { return []string{f.node, "-e", script} }
