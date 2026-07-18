package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// writeCustom points the loader at a fresh temp dir containing the
// given agents.json body, and resets the package state afterwards so
// tests can't leak custom agents into each other.
func writeCustom(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if body != "" {
		if err := os.WriteFile(filepath.Join(dir, CustomFileName), []byte(body), 0o600); err != nil {
			t.Fatalf("write %s: %v", CustomFileName, err)
		}
	}
	SetCustomDir(dir)
	t.Cleanup(func() { SetCustomDir("") })
	return dir
}

func findDef(defs []Def, id ID) (Def, bool) {
	for _, d := range defs {
		if d.ID == id {
			return d, true
		}
	}
	return Def{}, false
}

func TestLoadCustomAgents(t *testing.T) {
	writeCustom(t, `[
	  {"id": "claude-lite", "name": "Claude Lite", "cmd": ["claude", "--model", "haiku"], "color": "#8b5cf6"}
	]`)

	d, ok := Get("claude-lite")
	if !ok {
		t.Fatal("Get(claude-lite) = not found, want the custom agent")
	}
	if got, want := strings.Join(d.Cmd, " "), "claude --model haiku"; got != want {
		t.Errorf("Cmd = %q, want %q", got, want)
	}
	if d.Name != "Claude Lite" || d.Color != "#8b5cf6" {
		t.Errorf("Name/Color = %q/%q, want %q/%q", d.Name, d.Color, "Claude Lite", "#8b5cf6")
	}

	all := All()
	if _, ok := findDef(all, "claude-lite"); !ok {
		t.Error("All() omitted the custom agent")
	}
	// Custom agents sort after every built-in.
	if all[len(all)-1].ID != "claude-lite" {
		t.Errorf("All() last = %q, want the custom agent last", all[len(all)-1].ID)
	}
	if _, ok := findDef(all, IDClaude); !ok {
		t.Error("All() dropped the built-ins")
	}
}

func TestCustomAgentMalformedFileFallsBackToBuiltins(t *testing.T) {
	writeCustom(t, `[{"id": "broken",,,}`)

	all := All()
	if len(all) != len(displayOrder) {
		t.Errorf("All() = %d agents, want the %d built-ins", len(all), len(displayOrder))
	}
	if _, ok := Get(IDClaude); !ok {
		t.Error("built-in claude went missing after a malformed config")
	}
	if _, ok := Get("broken"); ok {
		t.Error("Get(broken) resolved from a malformed config")
	}
}

func TestCustomAgentMissingFileIsNotAnError(t *testing.T) {
	writeCustom(t, "") // dir exists, agents.json does not

	if len(All()) != len(displayOrder) {
		t.Errorf("All() = %d agents, want the %d built-ins", len(All()), len(displayOrder))
	}
	if _, ok := Get(IDClaude); !ok {
		t.Error("built-in claude went missing with no config file")
	}
}

func TestCustomAgentCannotShadowBuiltin(t *testing.T) {
	writeCustom(t, `[
	  {"id": "claude", "name": "Hijacked", "cmd": ["evil"]},
	  {"id": "mine", "name": "Mine", "cmd": ["mytool"]}
	]`)

	d, ok := Get(IDClaude)
	if !ok {
		t.Fatal("Get(claude) = not found")
	}
	if d.Name == "Hijacked" || d.Cmd[0] != "claude" {
		t.Errorf("built-in claude was shadowed: Name=%q Cmd=%v", d.Name, d.Cmd)
	}
	// The built-in must keep the Go func fields JSON can't express.
	if d.ResumeArgs == nil {
		t.Error("built-in claude lost ResumeArgs")
	}
	// A rejected entry must not take its valid siblings down with it.
	if _, ok := Get("mine"); !ok {
		t.Error("valid sibling entry was dropped alongside the rejected one")
	}
}

func TestCustomAgentSkipsInvalidEntries(t *testing.T) {
	writeCustom(t, `[
	  {"id": "", "name": "No ID", "cmd": ["a"]},
	  {"id": "no-cmd", "name": "No Cmd", "cmd": []},
	  {"id": "blank-cmd", "name": "Blank Cmd", "cmd": ["  "]},
	  {"id": "dupe", "name": "First", "cmd": ["first"]},
	  {"id": "dupe", "name": "Second", "cmd": ["second"]},
	  {"id": "good", "name": "Good", "cmd": ["goodtool", "--flag"]}
	]`)

	for _, id := range []ID{"", "no-cmd", "blank-cmd"} {
		if _, ok := Get(id); ok {
			t.Errorf("Get(%q) resolved, want rejected", id)
		}
	}
	// First one wins the duplicate id.
	if d, ok := Get("dupe"); !ok || d.Name != "First" {
		t.Errorf("Get(dupe) = %q/%v, want the first entry", d.Name, ok)
	}
	if d, ok := Get("good"); !ok || d.Cmd[1] != "--flag" {
		t.Errorf("Get(good) = %v/%v, want the valid entry to survive", d.Cmd, ok)
	}
}

func TestCustomAgentDefaultsNameAndColor(t *testing.T) {
	writeCustom(t, `[{"id": "bare", "cmd": ["bare"]}]`)

	d, ok := Get("bare")
	if !ok {
		t.Fatal("Get(bare) = not found")
	}
	if d.Name != "bare" {
		t.Errorf("Name = %q, want the id as fallback", d.Name)
	}
	if d.Color != defaultCustomColor {
		t.Errorf("Color = %q, want %q", d.Color, defaultCustomColor)
	}
}

func TestCustomAgentReloadsOnFileChange(t *testing.T) {
	dir := writeCustom(t, `[{"id": "v1", "name": "V1", "cmd": ["one"]}]`)

	if _, ok := Get("v1"); !ok {
		t.Fatal("Get(v1) = not found before the rewrite")
	}

	path := filepath.Join(dir, CustomFileName)
	// Same size as the original would make an mtime-only cache key
	// ambiguous on a coarse-resolution filesystem; the body below is
	// deliberately a different length.
	if err := os.WriteFile(path, []byte(`[{"id": "v2", "name": "Version Two", "cmd": ["two", "--x"]}]`), 0o600); err != nil {
		t.Fatalf("rewrite: %v", err)
	}

	if _, ok := Get("v1"); ok {
		t.Error("Get(v1) still resolves after the file changed")
	}
	d, ok := Get("v2")
	if !ok {
		t.Fatal("Get(v2) = not found after the rewrite")
	}
	if strings.Join(d.Cmd, " ") != "two --x" {
		t.Errorf("Cmd = %v, want the rewritten command", d.Cmd)
	}
}

// TestCustomAgentConcurrentAccess is a -race test: Get/All mutate the
// package-level cache, and hived calls them concurrently from
// Registry.Create and the CaptureSessionIDFn goroutines. It fails
// under `go test -race` if the cache mutex is dropped.
func TestCustomAgentConcurrentAccess(t *testing.T) {
	dir := writeCustom(t, `[{"id": "racer", "name": "Racer", "cmd": ["racer"]}]`)
	path := filepath.Join(dir, CustomFileName)

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				Get("racer")
				All()
			}
		})
	}
	// Rewrite underneath the readers so they race on the reload path,
	// not just the cache-hit path.
	wg.Go(func() {
		for n := range 20 {
			body := `[{"id": "racer", "name": "Racer", "cmd": ["racer", "` + strings.Repeat("x", n) + `"]}]`
			_ = os.WriteFile(path, []byte(body), 0o600)
		}
	})
	wg.Wait()
}

func TestSaveCustomAssignsStableIDs(t *testing.T) {
	writeCustom(t, "")

	list := []Custom{{Name: "Claude Lite", Cmd: []string{"claude", "--model", "haiku"}}}
	if err := SaveCustom(list); err != nil {
		t.Fatalf("SaveCustom: %v", err)
	}

	saved, err := LoadCustom()
	if err != nil {
		t.Fatalf("LoadCustom: %v", err)
	}
	if len(saved) != 1 || saved[0].ID != "claude-lite" {
		t.Fatalf("saved = %+v, want a claude-lite id slugged from the name", saved)
	}

	// The rename case: the ID must NOT be recomputed, or every session
	// persisted under the old id fails to revive.
	saved[0].Name = "Claude Litest"
	if err := SaveCustom(saved); err != nil {
		t.Fatalf("SaveCustom after rename: %v", err)
	}
	renamed, err := LoadCustom()
	if err != nil {
		t.Fatalf("LoadCustom after rename: %v", err)
	}
	if renamed[0].ID != "claude-lite" {
		t.Errorf("id = %q after rename, want it unchanged at %q", renamed[0].ID, "claude-lite")
	}
	if renamed[0].Name != "Claude Litest" {
		t.Errorf("name = %q, want the rename to have applied", renamed[0].Name)
	}
	if _, ok := Get("claude-lite"); !ok {
		t.Error("Get(claude-lite) broke after the rename")
	}
}

func TestSaveCustomAvoidsBuiltinAndDuplicateIDs(t *testing.T) {
	writeCustom(t, "")

	// Both entries slug to "claude", which is a built-in.
	list := []Custom{
		{Name: "Claude", Cmd: []string{"a"}},
		{Name: "claude", Cmd: []string{"b"}},
	}
	if err := SaveCustom(list); err != nil {
		t.Fatalf("SaveCustom: %v", err)
	}

	saved, err := LoadCustom()
	if err != nil {
		t.Fatalf("LoadCustom: %v", err)
	}
	if len(saved) != 2 {
		t.Fatalf("saved %d entries, want 2", len(saved))
	}
	for _, c := range saved {
		if c.ID == string(IDClaude) {
			t.Errorf("assigned the built-in id %q", c.ID)
		}
	}
	if saved[0].ID == saved[1].ID {
		t.Errorf("both entries got id %q", saved[0].ID)
	}
	if d, ok := Get(IDClaude); !ok || d.Name != "Claude" || d.Cmd[0] != "claude" {
		t.Error("built-in claude was disturbed by a colliding custom name")
	}
}

func TestSaveCustomRejectsInvalid(t *testing.T) {
	dir := writeCustom(t, `[{"id": "keeper", "name": "Keeper", "cmd": ["keeper"]}]`)
	path := filepath.Join(dir, CustomFileName)
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seed: %v", err)
	}

	cases := map[string][]Custom{
		"built-in collision": {{ID: "claude", Name: "Nope", Cmd: []string{"x"}}},
		"empty command":      {{ID: "empty", Name: "Empty", Cmd: nil}},
		"blank command":      {{ID: "blank", Name: "Blank", Cmd: []string{"   "}}},
		"duplicate id": {
			{ID: "same", Name: "One", Cmd: []string{"a"}},
			{ID: "same", Name: "Two", Cmd: []string{"b"}},
		},
		"unsluggable name": {{Name: "!!!", Cmd: []string{"x"}}},
		"missing name":     {{Name: "", Cmd: []string{"x"}}},
	}
	for name, list := range cases {
		t.Run(name, func(t *testing.T) {
			if err := SaveCustom(list); err == nil {
				t.Fatal("SaveCustom = nil, want a validation error")
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read after failed save: %v", err)
			}
			if string(after) != string(before) {
				t.Error("a rejected save modified the existing file")
			}
		})
	}
}

// A blank or unsluggable name must be reported as a name problem —
// the user never sees or types an id, so "missing id" would send them
// looking for a field that isn't in the form.
func TestSaveCustomNameErrorsMentionTheName(t *testing.T) {
	writeCustom(t, "")

	cases := map[string]struct {
		list []Custom
		want string
	}{
		"blank name":       {[]Custom{{Name: "  ", Cmd: []string{"x"}}}, "name is required"},
		"unsluggable name": {[]Custom{{Name: "!!!", Cmd: []string{"x"}}}, "letter or number"},
	}
	for label, tc := range cases {
		t.Run(label, func(t *testing.T) {
			err := SaveCustom(tc.list)
			if err == nil {
				t.Fatal("SaveCustom = nil, want an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			if strings.Contains(err.Error(), "missing id") {
				t.Errorf("error = %q, want it to blame the name, not the id", err)
			}
		})
	}
}

func TestSaveCustomWritesValidJSON(t *testing.T) {
	dir := writeCustom(t, "")

	list := []Custom{{Name: "My Tool", Cmd: []string{"mytool", "--fast"}, Color: "#123456"}}
	if err := SaveCustom(list); err != nil {
		t.Fatalf("SaveCustom: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, CustomFileName))
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var round []Custom
	if err := json.Unmarshal(raw, &round); err != nil {
		t.Fatalf("saved file is not valid JSON: %v", err)
	}
	if round[0].Color != "#123456" || round[0].Cmd[1] != "--fast" {
		t.Errorf("round-tripped = %+v, want the input preserved", round[0])
	}
	// No leftover temp files from the atomic write.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("dir has %d files, want only %s", len(entries), CustomFileName)
	}
}

func TestNoCustomDirIsInert(t *testing.T) {
	SetCustomDir("")
	if got, err := LoadCustom(); got != nil || err != nil {
		t.Errorf("LoadCustom() = %v, want nil with no dir set", got)
	}
	if err := SaveCustom([]Custom{{Name: "x", Cmd: []string{"x"}}}); err == nil {
		t.Error("SaveCustom = nil, want an error with no dir set")
	}
	if len(All()) != len(displayOrder) {
		t.Error("All() changed with no dir set")
	}
}

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Claude Lite":  "claude-lite",
		"  My  Tool  ": "my-tool",
		"GPT-4o!":      "gpt-4o",
		"!!!":          "",
		"":             "",
		"already-slug": "already-slug",
		"Ünïcödé Tool": "ünïcödé-tool",
		"日本語ツール":       "日本語ツール",
		"trailing---":  "trailing",
	} {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestLoadCustomSurfacesMalformedFile is the guard against silent data
// loss. customDefs deliberately degrades to built-ins on a broken
// file, but LoadCustom feeds the settings modal, where an empty list
// is indistinguishable from "no agents defined" — and saving that
// empty list back would overwrite the file the user is trying to fix.
func TestLoadCustomSurfacesMalformedFile(t *testing.T) {
	writeCustom(t, `[{"id":"broken","name":"Broken",,,}]`)

	got, err := LoadCustom()
	if err == nil {
		t.Fatal("LoadCustom = nil error on malformed JSON, want the parse failure surfaced")
	}
	if got != nil {
		t.Errorf("LoadCustom = %v, want nil entries alongside the error", got)
	}

	// The launcher still degrades quietly — the two paths differ on
	// purpose and this asserts they stay that way.
	if len(All()) != len(displayOrder) {
		t.Error("All() surfaced the broken file; the launcher must fall back to built-ins")
	}
}

// TestLoadCustomMissingFileIsNotAnError keeps "no file yet" (the
// first-run state) distinct from "file is broken".
func TestLoadCustomMissingFileIsNotAnError(t *testing.T) {
	SetCustomDir(t.TempDir())
	t.Cleanup(func() { SetCustomDir("") })

	got, err := LoadCustom()
	if err != nil {
		t.Errorf("LoadCustom = %v, want no error when agents.json does not exist", err)
	}
	if len(got) != 0 {
		t.Errorf("LoadCustom = %v, want no entries", got)
	}
}
