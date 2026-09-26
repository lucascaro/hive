package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/proc"
	"github.com/lucascaro/hive/internal/wire"
)

// TestHelperPlugin is not a test: it is the plugin process the manager
// tests spawn (the test binary re-executed with -test.run). It only
// acts when started by a Manager, which sets HIVE_PLUGIN_ID.
//
// Modes (the argument after "--"):
//
//	run    record pid + env in the data dir, then sleep
//	crash  exit 1 at once
//	tree   like run, but first start a child ("child") in the same group
//	child  record pid as child.pid, then sleep
func TestHelperPlugin(t *testing.T) {
	if os.Getenv("HIVE_PLUGIN_ID") == "" {
		return
	}
	mode := ""
	for i, a := range os.Args {
		if a == "--" && i+1 < len(os.Args) {
			mode = os.Args[i+1]
		}
	}
	data := os.Getenv("HIVE_PLUGIN_DATA_DIR")
	pidFile := "pid"
	if mode == "child" {
		pidFile = "child.pid"
	}
	_ = os.WriteFile(filepath.Join(data, pidFile), []byte(strconv.Itoa(os.Getpid())), 0o600)
	env, _ := json.Marshal(map[string]string{
		"HIVE_SOCKET":          os.Getenv("HIVE_SOCKET"),
		"HIVE_PLUGIN_ID":       os.Getenv("HIVE_PLUGIN_ID"),
		"HIVE_PLUGIN_API":      os.Getenv("HIVE_PLUGIN_API"),
		"HIVE_PLUGIN_DIR":      os.Getenv("HIVE_PLUGIN_DIR"),
		"HIVE_PLUGIN_DATA_DIR": data,
		"HIVE_SESSION_ID":      os.Getenv("HIVE_SESSION_ID"),
	})
	if mode != "child" {
		_ = os.WriteFile(filepath.Join(data, "env.json"), env, 0o600)
	}
	fmt.Println("helper plugin up:", mode)
	switch mode {
	case "crash":
		os.Exit(1)
	case "tree":
		exe, _ := os.Executable()
		c := proc.Command(exe, "-test.run=^TestHelperPlugin$", "--", "child")
		_ = c.Start()
	}
	time.Sleep(time.Hour)
	os.Exit(0)
}

// fakeListen stands in for the daemon's per-run plugin socket: it hands
// out distinct paths and counts closes, without a real listener.
type fakeListen struct {
	dir    string
	mu     sync.Mutex
	paths  []string
	closed int
}

func (f *fakeListen) listen(id string) (string, func(), error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	p := filepath.Join(f.dir, fmt.Sprintf("%s-%d.sock", id, len(f.paths)))
	f.paths = append(f.paths, p)
	return p, func() {
		f.mu.Lock()
		f.closed++
		f.mu.Unlock()
	}, nil
}

func (f *fakeListen) snapshot() ([]string, int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...), f.closed
}

// shrinkTimings makes supervision fast enough to test.
func shrinkTimings(t *testing.T) {
	t.Helper()
	oMin, oMax, oWin, oMax2, oGrace := backoffMin, backoffMax, crashWindow, maxCrashes, termGrace
	backoffMin, backoffMax, crashWindow, maxCrashes, termGrace = 10*time.Millisecond, 50*time.Millisecond, 10*time.Second, 3, 500*time.Millisecond
	t.Cleanup(func() {
		backoffMin, backoffMax, crashWindow, maxCrashes, termGrace = oMin, oMax, oWin, oMax2, oGrace
	})
}

func newTestManager(t *testing.T) (*Manager, *fakeListen) {
	t.Helper()
	shrinkTimings(t)
	fl := &fakeListen{dir: t.TempDir()}
	m, err := New(Config{StateDir: t.TempDir(), Listen: fl.listen})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	return m, fl
}

// writePlugin creates a plugin dir whose main command runs the helper
// in mode, and returns the dir.
func writePlugin(t *testing.T, id, mode string, edit func(*Manifest)) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	man := Manifest{
		ID: id, Name: "Test " + id, Version: "1.0.0", APIVersion: APIVersion,
		Main: &Entry{Command: []string{exe, "-test.run=^TestHelperPlugin$", "--", mode}},
	}
	if edit != nil {
		edit(&man)
	}
	dir := t.TempDir()
	b, _ := json.Marshal(man)
	if err := os.WriteFile(filepath.Join(dir, ManifestFile), b, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func install(t *testing.T, m *Manager, dir string) wire.PluginInfo {
	t.Helper()
	info, err := m.Install(context.Background(), dir, "")
	if err != nil {
		t.Fatalf("Install(%s): %v", dir, err)
	}
	return info
}

func find(m *Manager, id string) (wire.PluginInfo, bool) {
	for _, p := range m.List() {
		if p.ID == id {
			return p, true
		}
	}
	return wire.PluginInfo{}, false
}

// waitStatus polls until plugin id reports status, or fails.
func waitStatus(t *testing.T, m *Manager, id, status string) wire.PluginInfo {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		p, _ := find(m, id)
		if p.Status == status {
			return p
		}
		if time.Now().After(deadline) {
			t.Fatalf("plugin %s status = %q (%s), want %q", id, p.Status, p.StatusDetail, status)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readPid waits for the helper to record a pid in its data dir.
func readPid(t *testing.T, m *Manager, id, file string) int {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(m.DataPath(id), file))
		if err == nil {
			if pid, err := strconv.Atoi(string(b)); err == nil {
				return pid
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("no %s recorded for %s", file, id)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
