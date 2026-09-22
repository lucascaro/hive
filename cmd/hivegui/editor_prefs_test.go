package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSplitArgv(t *testing.T) {
	cases := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{`code -g {file}`, []string{"code", "-g", "{file}"}, false},
		{`  code    -g   {file}  `, []string{"code", "-g", "{file}"}, false},
		{`"/Applications/My Editor/bin/ed" {file}`, []string{"/Applications/My Editor/bin/ed", "{file}"}, false},
		{`ed --flag='a b' {file}`, []string{"ed", "--flag=a b", "{file}"}, false},
		{`ed ""  {file}`, []string{"ed", "", "{file}"}, false},
		{`ed "unterminated {file}`, nil, true},
		{`   `, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := splitArgv(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("splitArgv(%q) = %q, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("splitArgv(%q): %v", tc.in, err)
			}
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("splitArgv(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEditorArgv(t *testing.T) {
	cases := []struct {
		name     string
		settings EditorSettings
		line     int
		col      int
		want     []string
		wantErr  bool
	}{
		{"none", EditorSettings{}, 0, 0, nil, true},
		{"vscode with line and col", EditorSettings{Kind: editorVSCode}, 12, 5, []string{"code", "-g", "/f/a b.ts:12:5"}, false},
		{"vscode with line only", EditorSettings{Kind: editorVSCode}, 12, 0, []string{"code", "-g", "/f/a b.ts:12"}, false},
		{"vscode with no position", EditorSettings{Kind: editorVSCode}, 0, 0, []string{"code", "/f/a b.ts"}, false},
		{"cursor", EditorSettings{Kind: editorCursor}, 3, 0, []string{"cursor", "-g", "/f/a b.ts:3"}, false},
		{"zed", EditorSettings{Kind: editorZed}, 3, 4, []string{"zed", "/f/a b.ts:3:4"}, false},
		{"sublime", EditorSettings{Kind: editorSublime}, 3, 0, []string{"subl", "/f/a b.ts:3"}, false},
		{
			"command template keeps a spaced path as one argument",
			EditorSettings{Kind: editorCommand, Command: "nvim-qt +{line} {file}"}, 9, 0,
			[]string{"nvim-qt", "+9", "/f/a b.ts"}, false,
		},
		{
			"command template defaults a missing position to 1",
			EditorSettings{Kind: editorCommand, Command: "ed --line {line} --col {col} {file}"}, 0, 0,
			[]string{"ed", "--line", "1", "--col", "1", "/f/a b.ts"}, false,
		},
		{"app", EditorSettings{Kind: editorApp, App: "My Editor"}, 4, 0, []string{"open", "-a", "My Editor", "/f/a b.ts"}, false},
		{"unknown kind", EditorSettings{Kind: "emacs?"}, 0, 0, nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := editorArgv(tc.settings, "/f/a b.ts", tc.line, tc.col)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("editorArgv = %q, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if strings.Join(got, "\x00") != strings.Join(tc.want, "\x00") {
				t.Fatalf("editorArgv = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestEditorArgvTemplateCannotSplitOnFileName is the injection case:
// substitution happens after the argv split, so a file name can never
// introduce a new argument however it is spelled.
func TestEditorArgvTemplateCannotSplitOnFileName(t *testing.T) {
	s := EditorSettings{Kind: editorCommand, Command: "ed {file}"}
	got, err := editorArgv(s, `/f/a b; rm -rf ~ "$(whoami)".ts`, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("editorArgv = %q, want exactly 2 arguments", got)
	}
	if got[1] != `/f/a b; rm -rf ~ "$(whoami)".ts` {
		t.Fatalf("argument was rewritten: %q", got[1])
	}
}

// TestCheckBatchSafety covers the BatBadBut class of bug: Go runs a
// .cmd/.bat through cmd.exe and cannot escape its metacharacters, and
// `code` on Windows is normally code.cmd.
func TestCheckBatchSafety(t *testing.T) {
	evil := `C:\x\a&calc&.txt`
	if err := checkBatchSafety(`C:\Program Files\Code\bin\code.cmd`, []string{evil}); err == nil {
		t.Fatal("want a refusal: & is reinterpreted by cmd.exe")
	}
	if err := checkBatchSafety(`C:\x\ed.BAT`, []string{`C:\x\a|b.txt`}); err == nil {
		t.Fatal("want a refusal for |")
	}
	if err := checkBatchSafety(`C:\Code\Code.exe`, []string{evil}); err != nil {
		t.Fatalf("an .exe is spawned directly and needs no refusal: %v", err)
	}
	if err := checkBatchSafety(`C:\x\code.cmd`, []string{`C:\x\ordinary name.ts:12`}); err != nil {
		t.Fatalf("an ordinary path must still work: %v", err)
	}
}

func TestValidateEditorSettings(t *testing.T) {
	ok := []EditorSettings{
		{},
		{Kind: editorVSCode},
		{Kind: editorCommand, Command: "nvim-qt +{line} {file}"},
	}
	for _, s := range ok {
		if err := validateEditorSettings(s); err != nil {
			t.Fatalf("validate(%+v): %v", s, err)
		}
	}
	bad := []EditorSettings{
		{Kind: editorCommand},                                     // empty
		{Kind: editorCommand, Command: "code -g"},                 // no {file}
		{Kind: editorCommand, Command: `sh -c "ed {file}"`},       // a shell
		{Kind: editorCommand, Command: `/bin/bash -c {file}`},     // a shell by path
		{Kind: editorCommand, Command: `cmd /c start {file}`},     // a shell on Windows
		{Kind: editorCommand, Command: `powershell -c {file}`},    // ditto
		{Kind: editorCommand, Command: `open -a Terminal {file}`}, // launcher, not an editor
		{Kind: "nano-ish"}, // unknown kind
	}
	for _, s := range bad {
		if err := validateEditorSettings(s); err == nil {
			t.Fatalf("validate(%+v) = nil, want an error", s)
		}
	}
	appSettings := EditorSettings{Kind: editorApp, App: "My Editor"}
	err := validateEditorSettings(appSettings)
	if runtime.GOOS == "darwin" && err != nil {
		t.Fatalf("app kind on macOS: %v", err)
	}
	if runtime.GOOS != "darwin" && err == nil {
		t.Fatal("app kind is macOS-only, want an error elsewhere")
	}
}

func TestEditorSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	app := &App{}

	got, err := app.GetEditorSettings()
	if err != nil {
		t.Fatalf("a missing editor.json must read as defaults: %v", err)
	}
	if got.Kind != editorNone {
		t.Fatalf("default kind = %q, want empty", got.Kind)
	}

	want := EditorSettings{Kind: editorCommand, Command: "  nvim-qt +{line} {file}  "}
	if err := app.SaveEditorSettings(want); err != nil {
		t.Fatal(err)
	}
	got, err = app.GetEditorSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != editorCommand || got.Command != "nvim-qt +{line} {file}" {
		t.Fatalf("round trip = %+v", got)
	}
}

// TestLoadEditorSettingsCorruptIsError: a file the user hand-edited
// into invalid JSON must not read as "empty", or the next Save
// silently overwrites their config.
func TestLoadEditorSettingsCorruptIsError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	if err := os.WriteFile(filepath.Join(dir, "editor.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadEditorSettings(); err == nil {
		t.Fatal("want an error for a corrupt editor.json")
	}
}

func TestLoadEditorSettingsStripsBOM(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	body := "\ufeff{\"kind\":\"zed\"}"
	if err := os.WriteFile(filepath.Join(dir, "editor.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := loadEditorSettings()
	if err != nil {
		t.Fatal(err)
	}
	if got.Kind != editorZed {
		t.Fatalf("kind = %q, want zed", got.Kind)
	}
}
