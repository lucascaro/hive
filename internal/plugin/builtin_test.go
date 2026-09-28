package plugin

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/plugins"
)

// bundle is a one-plugin bundle whose ui.mjs holds body.
func bundle(body string) fstest.MapFS {
	return fstest.MapFS{
		"bundled/hive-plugin.json": {Data: []byte(`{"id":"bundled","name":"Bundled","version":"1.0.0","api_version":"` + APIVersion + `","ui":{"entry":"ui.mjs"}}`)},
		"bundled/ui.mjs":           {Data: []byte(body)},
	}
}

func newBuiltinManager(t *testing.T, stateDir string, fsys fstest.MapFS) *Manager {
	t.Helper()
	shrinkTimings(t)
	fl := &fakeListen{dir: t.TempDir()}
	m, err := New(Config{StateDir: stateDir, Listen: fl.listen, Builtin: fsys})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m.Stop)
	return m
}

func info(t *testing.T, m *Manager, id string) wire.PluginInfo {
	t.Helper()
	for _, p := range m.List() {
		if p.ID == id {
			return p
		}
	}
	t.Fatalf("%s is not listed", id)
	return wire.PluginInfo{}
}

// A fresh install lists every bundled plugin, installed and disabled,
// with nothing to fetch.
func TestEnsureBuiltins_FreshStateInstalledDisabled(t *testing.T) {
	state := t.TempDir()
	m := newBuiltinManager(t, state, bundle("export default () => ({})"))
	p := info(t, m, "bundled")
	if p.Enabled || !p.Builtin || p.Source != SourceBuiltin || p.Status != wire.PluginStopped {
		t.Errorf("fresh builtin = %+v, want installed, disabled, builtin", p)
	}
	if b, err := os.ReadFile(filepath.Join(state, installDir, "bundled", "ui.mjs")); err != nil || string(b) != "export default () => ({})" {
		t.Errorf("ui.mjs = %q, %v", b, err)
	}
}

// Every start rewrites stale files and drops ones the bundle no longer
// has, but the user's enabled choice survives.
func TestEnsureBuiltins_RewritesStaleFilesKeepsEnabled(t *testing.T) {
	state := t.TempDir()
	m := newBuiltinManager(t, state, bundle("v1"))
	if _, err := m.SetEnabled("bundled", true); err != nil {
		t.Fatal(err)
	}
	m.Stop()
	dir := filepath.Join(state, installDir, "bundled")
	if err := os.WriteFile(filepath.Join(dir, "leftover.js"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}

	m2 := newBuiltinManager(t, state, bundle("v2"))
	if b, _ := os.ReadFile(filepath.Join(dir, "ui.mjs")); string(b) != "v2" {
		t.Errorf("ui.mjs = %q, want the new bundle", b)
	}
	if _, err := os.Stat(filepath.Join(dir, "leftover.js")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a file the bundle dropped survived: %v", err)
	}
	if !info(t, m2, "bundled").Enabled {
		t.Error("an upgrade switched the user's enabled builtin off")
	}
}

// A crash between the aside rename and the cleanup leaves .<id>.old and
// a temp dir behind; the next start clears both.
func TestEnsureBuiltins_RecoversLeftoverAsideDir(t *testing.T) {
	state := t.TempDir()
	root := filepath.Join(state, installDir)
	for _, d := range []string{".bundled.old", ".builtin-bundled-123"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	newBuiltinManager(t, state, bundle("v1"))
	for _, d := range []string{".bundled.old", ".builtin-bundled-123"} {
		if _, err := os.Stat(filepath.Join(root, d)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s survived: %v", d, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "bundled", "ui.mjs")); err != nil {
		t.Errorf("bundle not installed: %v", err)
	}
}

func TestRemoveBuiltinRefused(t *testing.T) {
	state := t.TempDir()
	m := newBuiltinManager(t, state, bundle("v1"))
	if err := m.Remove("bundled"); !errors.Is(err, ErrBuiltin) {
		t.Fatalf("Remove = %v, want ErrBuiltin", err)
	}
	if _, err := os.Stat(filepath.Join(state, installDir, "bundled")); err != nil {
		t.Errorf("install dir gone: %v", err)
	}
}

func TestInstallBuiltinIDRefused(t *testing.T) {
	m := newBuiltinManager(t, t.TempDir(), bundle("v1"))
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, ManifestFile), []byte(`{"id":"bundled","name":"Impostor","version":"9.0.0","api_version":"`+APIVersion+`","ui":{"entry":"ui.mjs"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "ui.mjs"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Install(t.Context(), src, ""); !errors.Is(err, ErrBuiltin) {
		t.Fatalf("Install over a builtin = %v, want ErrBuiltin", err)
	}
	if got := info(t, m, "bundled").Name; got != "Bundled" {
		t.Errorf("the bundled plugin was replaced: name %q", got)
	}
}

// The plugin Hive actually ships: a valid, UI-only manifest for this API
// version, whose entry and style are in the bundle.
func TestBuiltinPlanReviewManifestValid(t *testing.T) {
	shrinkTimings(t)
	state := t.TempDir()
	fl := &fakeListen{dir: t.TempDir()}
	mgr, err := New(Config{StateDir: state, Listen: fl.listen, Builtin: plugins.Builtin})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Stop)
	p := info(t, mgr, "plan-review")
	if p.Status == wire.PluginRefused || p.UI == nil || len(p.Command) != 0 || !p.Builtin {
		t.Fatalf("plan-review = %+v, want a valid UI-only builtin", p)
	}
	for _, f := range []string{p.UI.Entry, p.UI.Style} {
		if st, err := os.Stat(filepath.Join(state, installDir, "plan-review", f)); err != nil || st.Size() == 0 {
			t.Errorf("%s missing or empty: %v", f, err)
		}
	}
}
