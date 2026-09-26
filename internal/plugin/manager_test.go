package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
