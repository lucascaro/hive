// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

func TestManager_ZeroPluginsSpawnsNothing(t *testing.T) {
	m, fl := newTestManager(t)
	m.Start()
	if n := m.RunnerCount(); n != 0 {
		t.Fatalf("RunnerCount = %d with no plugins, want 0", n)
	}
	// An installed but disabled plugin costs nothing either.
	install(t, m, writePlugin(t, "idle", "run", nil))
	time.Sleep(50 * time.Millisecond)
	if n := m.RunnerCount(); n != 0 {
		t.Fatalf("RunnerCount = %d with a disabled plugin, want 0", n)
	}
	if paths, _ := fl.snapshot(); len(paths) != 0 {
		t.Fatalf("opened %d plugin sockets with nothing enabled", len(paths))
	}
}

func TestInstall_LandsDisabledAndNeverRuns(t *testing.T) {
	m, _ := newTestManager(t)
	m.Start()
	info := install(t, m, writePlugin(t, "quiet", "run", nil))
	if info.Enabled || info.Status != wire.PluginStopped {
		t.Fatalf("installed plugin = enabled %v status %q, want disabled + stopped", info.Enabled, info.Status)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(filepath.Join(m.DataPath("quiet"), "pid")); err == nil {
		t.Fatal("a freshly installed plugin ran before being enabled")
	}
}

func TestManager_EnableStartsDisableStops(t *testing.T) {
	m, fl := newTestManager(t)
	m.Start()
	install(t, m, writePlugin(t, "svc", "run", nil))
	if _, err := m.SetEnabled("svc", true); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, "svc", wire.PluginRunning)
	pid := readPid(t, m, "svc", "pid")

	var env map[string]string
	b, _ := os.ReadFile(filepath.Join(m.DataPath("svc"), "env.json"))
	_ = json.Unmarshal(b, &env)
	paths, _ := fl.snapshot()
	if env["HIVE_SOCKET"] != paths[0] {
		t.Errorf("HIVE_SOCKET = %q, want the per-run socket %q", env["HIVE_SOCKET"], paths[0])
	}
	if env["HIVE_PLUGIN_ID"] != "svc" || env["HIVE_PLUGIN_API"] != APIVersion || env["HIVE_SESSION_ID"] != "" {
		t.Errorf("plugin env = %v", env)
	}

	info, err := m.SetEnabled("svc", false)
	if err != nil {
		t.Fatal(err)
	}
	if info.Status != wire.PluginStopped || m.RunnerCount() != 0 {
		t.Fatalf("after disable: status %q, runners %d", info.Status, m.RunnerCount())
	}
	assertDead(t, pid)
	if _, closed := fl.snapshot(); closed != 1 {
		t.Errorf("plugin socket closed %d times, want 1", closed)
	}
}

func TestManager_CrashRestartsWithBackoff(t *testing.T) {
	m, fl := newTestManager(t)
	maxCrashes = 100 // never fail within this test
	m.Start()
	install(t, m, writePlugin(t, "flaky", "crash", nil))
	if _, err := m.SetEnabled("flaky", true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		p, _ := find(m, "flaky")
		if p.Restarts >= 3 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("restarts = %d after 10s, want >= 3", p.Restarts)
		}
		time.Sleep(10 * time.Millisecond)
	}
	paths, _ := fl.snapshot()
	seen := map[string]bool{}
	for _, p := range paths {
		if seen[p] {
			t.Fatalf("socket path %s reused across restarts", p)
		}
		seen[p] = true
	}
}

func TestManager_CrashLoopMarksFailed(t *testing.T) {
	m, _ := newTestManager(t)
	m.Start()
	install(t, m, writePlugin(t, "doomed", "crash", nil))
	if _, err := m.SetEnabled("doomed", true); err != nil {
		t.Fatal(err)
	}
	p := waitStatus(t, m, "doomed", wire.PluginFailed)
	if p.Restarts != maxCrashes+1 || !strings.Contains(p.StatusDetail, "exits within") {
		t.Fatalf("failed plugin: restarts %d detail %q", p.Restarts, p.StatusDetail)
	}
	deadline := time.Now().Add(5 * time.Second)
	for m.RunnerCount() != 0 {
		if time.Now().After(deadline) {
			t.Fatal("failed plugin still has a supervisor")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Re-enabling starts it afresh.
	if _, err := m.SetEnabled("doomed", true); err != nil {
		t.Fatal(err)
	}
	if p, _ := find(m, "doomed"); p.Restarts > 1 {
		t.Errorf("re-enable kept restarts = %d", p.Restarts)
	}
}

func TestManager_RefusedAPIVersionNeverSpawns(t *testing.T) {
	m, fl := newTestManager(t)
	m.Start()
	info := install(t, m, writePlugin(t, "future", "run", func(man *Manifest) { man.APIVersion = "9.0" }))
	if info.Status != wire.PluginRefused || !strings.Contains(info.StatusDetail, "9.0") {
		t.Fatalf("install of a 9.0 plugin: status %q detail %q", info.Status, info.StatusDetail)
	}
	if _, err := m.SetEnabled("future", true); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, "future", wire.PluginRefused)
	if paths, _ := fl.snapshot(); len(paths) != 0 {
		t.Fatal("a refused plugin got a socket")
	}
}

func TestManager_MissingCommandRefusedNotCrashLoop(t *testing.T) {
	m, fl := newTestManager(t)
	m.Start()
	install(t, m, writePlugin(t, "nonode", "run", func(man *Manifest) {
		man.Main.Command = []string{"hive-no-such-binary-xyz", "main.mjs"}
	}))
	if _, err := m.SetEnabled("nonode", true); err != nil {
		t.Fatal(err)
	}
	p := waitStatus(t, m, "nonode", wire.PluginRefused)
	if !strings.Contains(p.StatusDetail, "not found on PATH=") || p.Restarts != 0 {
		t.Fatalf("missing command: detail %q restarts %d", p.StatusDetail, p.Restarts)
	}
	if paths, _ := fl.snapshot(); len(paths) != 0 {
		t.Fatal("a plugin with no command got a socket")
	}
}

func TestManager_RemoveStopsAndKeepsDataDir(t *testing.T) {
	m, _ := newTestManager(t)
	m.Start()
	install(t, m, writePlugin(t, "gone", "run", nil))
	if _, err := m.SetEnabled("gone", true); err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, m, "gone", "pid")
	if err := m.Remove("gone"); err != nil {
		t.Fatal(err)
	}
	assertDead(t, pid)
	if _, ok := find(m, "gone"); ok {
		t.Fatal("removed plugin still listed")
	}
	if _, err := os.Stat(m.installPath("gone")); !os.IsNotExist(err) {
		t.Fatalf("install dir still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.DataPath("gone"), "pid")); err != nil {
		t.Fatalf("data dir was deleted: %v", err)
	}
	if err := m.Remove("gone"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second Remove = %v, want ErrNotFound", err)
	}
}

func TestManager_StopKillsProcessTree(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX process-group semantics")
	}
	m, _ := newTestManager(t)
	m.Start()
	install(t, m, writePlugin(t, "forker", "tree", nil))
	if _, err := m.SetEnabled("forker", true); err != nil {
		t.Fatal(err)
	}
	pid := readPid(t, m, "forker", "pid")
	child := readPid(t, m, "forker", "child.pid")
	m.Stop()
	assertDead(t, pid)
	assertDead(t, child)
}

func TestManager_EnableAfterStopSpawnsNothing(t *testing.T) {
	m, fl := newTestManager(t)
	m.Start()
	install(t, m, writePlugin(t, "late", "run", nil))
	m.Stop()
	if _, err := m.SetEnabled("late", true); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("SetEnabled after Stop = %v, want ErrManagerStopped", err)
	}
	if _, err := m.Install(context.Background(), writePlugin(t, "later", "run", nil), ""); !errors.Is(err, ErrManagerStopped) {
		t.Fatalf("Install after Stop = %v, want ErrManagerStopped", err)
	}
	if paths, _ := fl.snapshot(); len(paths) != 0 || m.RunnerCount() != 0 {
		t.Fatal("something spawned after Stop")
	}
}

func TestManager_PersistsAcrossRestart(t *testing.T) {
	shrinkTimings(t)
	state := t.TempDir()
	fl := &fakeListen{dir: t.TempDir()}
	m1, _ := New(Config{StateDir: state, Listen: fl.listen})
	info, err := m1.Install(context.Background(), writePlugin(t, "keep", "run", nil), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m1.SetEnabled("keep", true); err != nil {
		t.Fatal(err)
	}
	m1.Stop()

	m2, err := New(Config{StateDir: state, Listen: fl.listen})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m2.Stop)
	got, ok := find(m2, "keep")
	if !ok || !got.Enabled || got.Source != info.Source || got.Name != "Test keep" {
		t.Fatalf("reloaded plugin = %+v", got)
	}
	m2.Start()
	waitStatus(t, m2, "keep", wire.PluginRunning)
}

func TestManager_EventsFanOut(t *testing.T) {
	m, _ := newTestManager(t)
	ch, unsub := m.Subscribe()
	defer unsub()
	m.Start()
	if _, err := m.Install(context.Background(), writePlugin(t, "ev", "run", nil), "nonce-1"); err != nil {
		t.Fatal(err)
	}
	select {
	case ev := <-ch:
		if ev.Kind != wire.PluginEventAdded || ev.Nonce != "nonce-1" || ev.Plugin.ID != "ev" {
			t.Fatalf("first event = %+v", ev)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no added event")
	}
	if err := m.Remove("ev"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-ch:
			if ev.Kind == wire.PluginEventRemoved {
				return
			}
		case <-deadline:
			t.Fatal("no removed event")
		}
	}
}

// Remove racing an enable must never leave a runner Stop cannot reach:
// if it did, Stop would wait on it forever (daemon shutdown hangs) and
// the plugin process would leak.
func TestManager_RemoveRacingEnableLeavesNoOrphan(t *testing.T) {
	for i := range 20 {
		m, _ := newTestManager(t)
		m.Start()
		install(t, m, writePlugin(t, "race", "run", nil))
		if _, err := m.SetEnabled("race", true); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{}, 2)
		go func() { _ = m.Remove("race"); done <- struct{}{} }()
		go func() { _, _ = m.SetEnabled("race", true); done <- struct{}{} }()
		<-done
		<-done
		stopped := make(chan struct{})
		go func() { m.Stop(); close(stopped) }()
		select {
		case <-stopped:
		case <-time.After(10 * time.Second):
			t.Fatalf("iteration %d: Stop hung on an orphaned runner", i)
		}
	}
}

// Concurrent disable and enable must end consistent: an enabled plugin
// is running, a disabled one is not.
func TestManager_DisableRacingEnableEndsConsistent(t *testing.T) {
	m, _ := newTestManager(t)
	m.Start()
	install(t, m, writePlugin(t, "flip", "run", nil))
	for i := range 20 {
		if _, err := m.SetEnabled("flip", true); err != nil {
			t.Fatal(err)
		}
		done := make(chan struct{}, 2)
		go func() { _, _ = m.SetEnabled("flip", false); done <- struct{}{} }()
		go func() { _, _ = m.SetEnabled("flip", true); done <- struct{}{} }()
		<-done
		<-done
		p, _ := find(m, "flip")
		if p.Enabled != (m.RunnerCount() == 1) {
			t.Fatalf("iteration %d: enabled=%v but %d runner(s)", i, p.Enabled, m.RunnerCount())
		}
	}
}

// Disabling a plugin while the daemon shuts down must not close the
// runner's stop channel twice (a panic, not a clean shutdown).
func TestManager_DisableRacingStopNoPanic(t *testing.T) {
	for range 20 {
		m, _ := newTestManager(t)
		m.Start()
		install(t, m, writePlugin(t, "down", "run", nil))
		if _, err := m.SetEnabled("down", true); err != nil {
			t.Fatal(err)
		}
		waitStatus(t, m, "down", wire.PluginRunning)
		done := make(chan struct{})
		go func() { _, _ = m.SetEnabled("down", false); close(done) }()
		m.Stop()
		<-done
	}
}

// A remove whose plugins.json write fails leaves the plugin installed —
// in the list and on disk — rather than vanishing until the next start.
func TestManager_RemoveSaveFailureKeepsPlugin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	m, _ := newTestManager(t)
	install(t, m, writePlugin(t, "stay", "run", nil))
	if err := os.Chmod(m.stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(m.stateDir, 0o700)
	if err := m.Remove("stay"); err == nil {
		t.Fatal("Remove succeeded with an unwritable state dir")
	}
	if _, ok := find(m, "stay"); !ok {
		t.Fatal("plugin vanished from the list after a failed remove")
	}
	if _, err := os.Stat(m.installPath("stay")); err != nil {
		t.Fatalf("install dir deleted after a failed remove: %v", err)
	}
}

// A SetEnabled whose plugins.json write fails restores the flag it
// found, even when the call asked for the value it already had.
func TestManager_SetEnabledSaveFailureRestoresFlag(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission bits")
	}
	m, _ := newTestManager(t)
	install(t, m, writePlugin(t, "same", "run", nil))
	if err := os.Chmod(m.stateDir, 0o500); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(m.stateDir, 0o700)
	if _, err := m.SetEnabled("same", false); err == nil {
		t.Fatal("SetEnabled succeeded with an unwritable state dir")
	}
	if p, _ := find(m, "same"); p.Enabled {
		t.Fatal("failed redundant disable left the plugin marked enabled")
	}
}

// An enable racing Stop must never leave a runner behind once Stop has
// returned: that would be a plugin process outliving daemon shutdown.
func TestManager_EnableRacingStopNoRunnerAfterStop(t *testing.T) {
	for i := range 50 {
		m, fl := newTestManager(t)
		m.Start()
		install(t, m, writePlugin(t, "late", "run", nil))
		done := make(chan struct{})
		go func() { _, _ = m.SetEnabled("late", true); close(done) }()
		m.Stop()
		<-done
		time.Sleep(20 * time.Millisecond)
		if n := m.RunnerCount(); n != 0 {
			paths, _ := fl.snapshot()
			t.Fatalf("iteration %d: %d runner(s) after Stop returned (sockets %v)", i, n, paths)
		}
	}
}

// fakeNode writes an executable "node" that prints out and exits with
// code, and returns its directory. Unix only: the Windows probe needs a
// real .exe.
func fakeNode(t *testing.T, out string, code int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake node is a shell script")
	}
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\necho %q\nexit %d\n", out, code)
	if err := os.WriteFile(filepath.Join(dir, "node"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// withLoginPATH empties the daemon's own PATH and makes dir the login
// PATH.
func withLoginPATH(t *testing.T, dir string) {
	t.Helper()
	t.Setenv("PATH", t.TempDir())
	old := loginPATH
	loginPATH = func() string { return dir }
	t.Cleanup(func() { loginPATH = old })
}

func TestResolveCommand_LoginPATHFallback(t *testing.T) {
	login := fakeNode(t, "v26.0.0", 0)
	withLoginPATH(t, login)
	bin, pathEnv, err := resolveCommand(t.TempDir(), "node")
	if err != nil || bin != filepath.Join(login, "node") || pathEnv != login {
		t.Fatalf("resolveCommand = %q, %q, %v; want the login PATH's node", bin, pathEnv, err)
	}
	if _, _, err := resolveCommand(t.TempDir(), "hive-no-such-binary-xyz"); err == nil ||
		!strings.Contains(err.Error(), "login shell's PATH="+login) {
		t.Fatalf("a miss must name both PATHs, got %v", err)
	}
}

func TestResolveCommand_DaemonPATHWins(t *testing.T) {
	daemon := fakeNode(t, "v26.0.0", 0)
	login := fakeNode(t, "v26.0.0", 0)
	withLoginPATH(t, login)
	t.Setenv("PATH", daemon)
	bin, pathEnv, err := resolveCommand(t.TempDir(), "node")
	if err != nil || bin != filepath.Join(daemon, "node") || pathEnv != daemon {
		t.Fatalf("resolveCommand = %q, %q, %v; want the daemon PATH's node", bin, pathEnv, err)
	}
}

func TestPluginEnv_PATHIsResolvedPATH(t *testing.T) {
	got := pluginEnv([]string{"PATH=/old", "KEEP=1", "HIVE_PLUGIN_ID=x"}, "PATH=/new", "HIVE_PLUGIN_ID=y")
	var paths []string
	for _, kv := range got {
		if strings.HasPrefix(kv, "PATH=") {
			paths = append(paths, kv)
		}
	}
	if len(paths) != 1 || paths[0] != "PATH=/new" || !slices.Contains(got, "KEEP=1") || slices.Contains(got, "HIVE_PLUGIN_ID=x") {
		t.Fatalf("env = %v", got)
	}
}

// nodePlugin installs and enables a plugin that declares a Node floor;
// its main is the test binary, so argv[0] is not node.
func nodePlugin(t *testing.T, m *Manager, id, floor string) {
	t.Helper()
	install(t, m, writePlugin(t, id, "run", func(man *Manifest) { man.Node = floor }))
	if _, err := m.SetEnabled(id, true); err != nil {
		t.Fatal(err)
	}
}

func TestSpawn_NodeTooOldRefused(t *testing.T) {
	withLoginPATH(t, fakeNode(t, "v24.1.0", 0))
	m, fl := newTestManager(t)
	m.Start()
	nodePlugin(t, m, "old", "26.0.0")
	p := waitStatus(t, m, "old", wire.PluginRefused)
	if !strings.Contains(p.StatusDetail, "requires Node.js 26.0.0 or newer (found v24.1.0 at ") || p.Restarts != 0 {
		t.Fatalf("detail %q restarts %d", p.StatusDetail, p.Restarts)
	}
	if paths, _ := fl.snapshot(); len(paths) != 0 {
		t.Fatal("a refused plugin got a socket")
	}
}

func TestSpawn_NodeProbeFails(t *testing.T) {
	withLoginPATH(t, fakeNode(t, "boom", 1))
	m, _ := newTestManager(t)
	m.Start()
	nodePlugin(t, m, "broken", "26.0.0")
	p := waitStatus(t, m, "broken", wire.PluginRefused)
	if !strings.Contains(p.StatusDetail, "could not run node --version") {
		t.Fatalf("detail %q", p.StatusDetail)
	}
}

func TestSpawn_NodeMissingRefused(t *testing.T) {
	withLoginPATH(t, t.TempDir())
	m, _ := newTestManager(t)
	m.Start()
	nodePlugin(t, m, "nonode2", "26.0.0")
	p := waitStatus(t, m, "nonode2", wire.PluginRefused)
	if !strings.Contains(p.StatusDetail, "node not found on PATH=") {
		t.Fatalf("detail %q", p.StatusDetail)
	}
}

// A refused plugin recovers once Node is upgraded and the user presses
// Restart (SetEnabled true on an already enabled plugin).
func TestSpawn_NodeOKAfterUpgradeAndRestart(t *testing.T) {
	login := fakeNode(t, "v24.1.0", 0)
	withLoginPATH(t, login)
	m, _ := newTestManager(t)
	m.Start()
	nodePlugin(t, m, "upg", "26.0.0")
	waitStatus(t, m, "upg", wire.PluginRefused)
	if err := os.WriteFile(filepath.Join(login, "node"), []byte("#!/bin/sh\necho v26.2.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := m.SetEnabled("upg", true); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, "upg", wire.PluginRunning)
}

func TestHeadlessBuiltin_Starts(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	man, _ := json.Marshal(Manifest{
		ID: "hl", Name: "Headless", Version: "1.0.0", APIVersion: APIVersion,
		Main: &Entry{Command: []string{exe, "-test.run=^TestHelperPlugin$", "--", "run"}},
	})
	m := newBuiltinManager(t, t.TempDir(), fstest.MapFS{"hl/hive-plugin.json": {Data: man}})
	m.Start()
	if _, err := m.SetEnabled("hl", true); err != nil {
		t.Fatal(err)
	}
	waitStatus(t, m, "hl", wire.PluginRunning)
}
