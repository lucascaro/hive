package plugin

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// uiOnly turns a writePlugin manifest into a plugin with only a UI entry.
func uiOnly(m *Manifest) {
	m.Main = nil
	m.UI = &UIEntry{Entry: "ui.mjs", Style: "ui.css"}
}

func TestManifest_UIOnlyValid(t *testing.T) {
	m := Manifest{ID: "notes", Name: "Notes", APIVersion: APIVersion, UI: &UIEntry{Entry: "ui.mjs"}}
	if err := m.Validate(); err != nil {
		t.Fatalf("ui-only manifest: %v", err)
	}
	m.Main = &Entry{Command: []string{"node", "main.mjs"}}
	m.UI = &UIEntry{Entry: "dist/ui.js", Style: "dist/ui.css"}
	if err := m.Validate(); err != nil {
		t.Fatalf("main + ui manifest: %v", err)
	}
}

func TestManifest_RequiresMainOrUI(t *testing.T) {
	m := Manifest{ID: "notes", Name: "Notes", APIVersion: APIVersion}
	if err := m.Validate(); err == nil || !strings.Contains(err.Error(), `"main", "ui"`) {
		t.Fatalf("no main, no ui: Validate = %v", err)
	}
}

func TestManifest_UIEntryRejects(t *testing.T) {
	for name, ui := range map[string]UIEntry{
		"Traversal":    {Entry: "../other/ui.js"},
		"Absolute":     {Entry: "/etc/ui.js"},
		"URL":          {Entry: "https://example.com/ui.js"},
		"DataURL":      {Entry: "data:text/javascript,1"},
		"Backslash":    {Entry: `a\ui.js`},
		"NonJS":        {Entry: "ui.ts"},
		"Empty":        {Entry: ""},
		"DotSlash":     {Entry: "./ui.js"},
		"StyleNotCSS":  {Entry: "ui.js", Style: "ui.js"},
		"StyleEscapes": {Entry: "ui.js", Style: "../x.css"},
		"Newline":      {Entry: "ui\n.js"},
	} {
		m := Manifest{ID: "notes", Name: "Notes", APIVersion: APIVersion, UI: &ui}
		if err := m.Validate(); err == nil || errors.Is(err, ErrAPIVersion) {
			t.Errorf("%s (%+v): Validate = %v, want a structural error", name, ui, err)
		}
	}
}

// A 0.1 plugin is refused, not rejected: it installs and says why it
// will not run.
func TestManifest_API01Refused(t *testing.T) {
	m, _ := newTestManager(t)
	info := install(t, m, writePlugin(t, "old", "run", func(man *Manifest) { man.APIVersion = "0.1" }))
	if info.Status != wire.PluginRefused || !strings.Contains(info.StatusDetail, `"0.1"`) {
		t.Fatalf("0.1 plugin: status %q detail %q, want refused naming 0.1", info.Status, info.StatusDetail)
	}
}

func TestManager_UIOnlyEnableSpawnsNothing(t *testing.T) {
	m, fl := newTestManager(t)
	m.Start()
	info := install(t, m, writePlugin(t, "notes", "", uiOnly))
	if info.UI == nil || info.UI.Entry != "ui.mjs" || info.UI.Style != "ui.css" {
		t.Fatalf("PluginInfo.UI = %+v", info.UI)
	}
	if len(info.Command) != 0 {
		t.Fatalf("ui-only plugin reports a command: %v", info.Command)
	}
	on, err := m.SetEnabled("notes", true)
	if err != nil {
		t.Fatal(err)
	}
	if on.Status != wire.PluginRunning {
		t.Fatalf("enabled ui-only status %q, want running", on.Status)
	}
	if n := m.RunnerCount(); n != 0 {
		t.Fatalf("RunnerCount = %d, want 0", n)
	}
	if socks, _ := fl.snapshot(); len(socks) != 0 {
		t.Fatalf("ui-only plugin opened sockets: %v", socks)
	}
	off, err := m.SetEnabled("notes", false)
	if err != nil {
		t.Fatal(err)
	}
	if off.Status != wire.PluginStopped {
		t.Fatalf("disabled ui-only status %q, want stopped", off.Status)
	}
}

// An enabled UI-only plugin reports running again after a daemon restart.
func TestManager_UIOnlyRunningAfterRestart(t *testing.T) {
	m, fl := newTestManager(t)
	install(t, m, writePlugin(t, "notes", "", uiOnly))
	if _, err := m.SetEnabled("notes", true); err != nil {
		t.Fatal(err)
	}
	m.Stop()
	m2, err := New(Config{StateDir: m.stateDir, Listen: fl.listen})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m2.Stop)
	m2.Start()
	if p, _ := find(m2, "notes"); p.Status != wire.PluginRunning {
		t.Fatalf("after restart status %q, want running", p.Status)
	}
}

func TestManager_SetConfigPersistsAndEmits(t *testing.T) {
	m, _ := newTestManager(t)
	install(t, m, writePlugin(t, "notes", "", uiOnly))
	events, done := m.Subscribe()
	defer done()
	info, err := m.SetConfig("notes", json.RawMessage(`{ "badge": true, "notes": {"s1": "hi"} }`))
	if err != nil {
		t.Fatal(err)
	}
	want := `{"badge":true,"notes":{"s1":"hi"}}`
	if string(info.Config) != want {
		t.Fatalf("returned config %s, want %s", info.Config, want)
	}
	ev := <-events
	if ev.Kind != wire.PluginEventUpdated || string(ev.Plugin.Config) != want {
		t.Fatalf("event %s config %s", ev.Kind, ev.Plugin.Config)
	}
	b, err := os.ReadFile(filepath.Join(m.DataPath("notes"), uiConfigFile))
	if err != nil || string(b) != want {
		t.Fatalf("ui-config.json = %q, %v", b, err)
	}
	if p, _ := find(m, "notes"); string(p.Config) != want {
		t.Fatalf("List config %s", p.Config)
	}
}

func TestManager_SetConfigRejects(t *testing.T) {
	m, _ := newTestManager(t)
	install(t, m, writePlugin(t, "notes", "", uiOnly))
	big := `{"x":"` + strings.Repeat("a", wire.MaxPluginConfig) + `"}`
	for name, raw := range map[string]string{
		"Oversize":    big,
		"InvalidJSON": `{"x":`,
		"NotObject":   `[1,2]`,
		"Null":        `null`,
	} {
		if _, err := m.SetConfig("notes", json.RawMessage(raw)); err == nil {
			t.Errorf("%s: SetConfig accepted", name)
		}
	}
	if _, err := m.SetConfig("nope", json.RawMessage(`{}`)); !errors.Is(err, ErrNotFound) {
		t.Errorf("UnknownID: err %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(m.DataPath("notes"), uiConfigFile)); !os.IsNotExist(err) {
		t.Errorf("a rejected config was written: %v", err)
	}
}

func TestManager_ConfigSurvivesReinstall(t *testing.T) {
	m, _ := newTestManager(t)
	dir := writePlugin(t, "notes", "", uiOnly)
	install(t, m, dir)
	if _, err := m.SetConfig("notes", json.RawMessage(`{"a":1}`)); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("notes"); err != nil {
		t.Fatal(err)
	}
	if info := install(t, m, dir); string(info.Config) != `{"a":1}` {
		t.Fatalf("config after reinstall: %s", info.Config)
	}
}

// A headless plugin's own config.json (the webhook keeps its URL, often
// with a token, there) is never sent to clients and never overwritten.
func TestManager_HeadlessConfigNeitherExposedNorClobbered(t *testing.T) {
	m, _ := newTestManager(t)
	install(t, m, writePlugin(t, "hook", "run", nil))
	data := m.DataPath("hook")
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	secret := `{"url":"https://example.com/hook?token=SECRET"}`
	for _, f := range []string{"config.json", uiConfigFile} {
		if err := os.WriteFile(filepath.Join(data, f), []byte(secret), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	m.Stop()
	m2, err := New(Config{StateDir: m.stateDir, Listen: (&fakeListen{dir: t.TempDir()}).listen})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m2.Stop)
	p, _ := find(m2, "hook")
	if p.Config != nil || p.UI != nil {
		t.Fatalf("headless plugin exposes config %s / ui %+v", p.Config, p.UI)
	}
	b, _ := json.Marshal(m2.List())
	if strings.Contains(string(b), "SECRET") {
		t.Fatalf("List leaks the plugin's config: %s", b)
	}
	if _, err := m2.SetConfig("hook", json.RawMessage(`{}`)); !errors.Is(err, ErrNoUI) {
		t.Fatalf("SetConfig on headless plugin: %v, want ErrNoUI", err)
	}
	if b, _ := os.ReadFile(filepath.Join(data, "config.json")); string(b) != secret {
		t.Fatalf("config.json changed: %s", b)
	}
}

// A hand-edited, invalid ui-config.json reads as no config instead of
// failing the plugin list.
func TestManager_InvalidUIConfigOnDiskIgnored(t *testing.T) {
	m, fl := newTestManager(t)
	install(t, m, writePlugin(t, "notes", "", uiOnly))
	data := m.DataPath("notes")
	_ = os.MkdirAll(data, 0o700)
	if err := os.WriteFile(filepath.Join(data, uiConfigFile), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	m.Stop()
	m2, err := New(Config{StateDir: m.stateDir, Listen: fl.listen})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(m2.Stop)
	if p, ok := find(m2, "notes"); !ok || p.Config != nil {
		t.Fatalf("invalid ui-config.json: found %v config %s", ok, p.Config)
	}
}
