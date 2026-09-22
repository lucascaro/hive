// The editor behind ⇧⌘-click on a path in a session, and its
// on-disk settings.
//
// The setting lives in a GUI-owned JSON file rather than localStorage
// for one reason: the command Hive executes is decided here, from a
// file, not handed over per call by the renderer. The bridge method
// (OpenFile) carries a path and a line number and nothing else.
//
// $EDITOR is deliberately not consulted: it is almost always a
// terminal editor, and launching vim with no terminal from a GUI
// click does nothing a user would recognise as success.
package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lucascaro/hive/internal/proc"
	"github.com/lucascaro/hive/internal/registry"
)

// EditorSettings is the on-disk shape of <stateDir>/editor.json and
// the payload of the GetEditorSettings/SaveEditorSettings bindings.
//
// Kind selects which of the other fields matters: "command" uses
// Command, "app" uses App, a preset uses neither, and "" means no
// editor is configured (⇧⌘-click then behaves like ⌘-click).
type EditorSettings struct {
	Kind    string `json:"kind"`
	Command string `json:"command"`
	App     string `json:"app"`
}

const (
	editorNone    = ""
	editorVSCode  = "vscode"
	editorCursor  = "cursor"
	editorZed     = "zed"
	editorSublime = "sublime"
	editorCommand = "command"
	editorApp     = "app"
)

var (
	errNoEditorConfigured = errors.New("no editor configured")
	errEditorNotFound     = errors.New("editor not found on PATH")
)

func editorSettingsPath() string {
	return filepath.Join(registry.StateDir(), "editor.json")
}

// loadEditorSettings reads editor.json. A missing file means "no
// editor configured", not an error. A file that exists but will not
// parse *is* an error, so a later save cannot overwrite a config the
// user hand-edited and mistyped — the same rule as update.json.
func loadEditorSettings() (EditorSettings, error) {
	b, err := os.ReadFile(editorSettingsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return EditorSettings{}, nil
		}
		return EditorSettings{}, fmt.Errorf("read editor.json: %w", err)
	}
	// PowerShell and Notepad write UTF-8 with a BOM and encoding/json
	// will not skip one; drop it rather than failing the load.
	b = bytes.TrimPrefix(b, []byte("\ufeff"))
	var s EditorSettings
	if err := json.Unmarshal(b, &s); err != nil {
		return EditorSettings{}, fmt.Errorf("parse %s: %w", editorSettingsPath(), err)
	}
	return s, nil
}

func saveEditorSettings(s EditorSettings) error {
	dir := registry.StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, "editor-*.json")
	if err != nil {
		return fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename succeeds
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp: %w", err)
	}
	if err := os.Rename(tmpName, editorSettingsPath()); err != nil {
		return fmt.Errorf("rename editor.json: %w", err)
	}
	return nil
}

// GetEditorSettings is the Wails binding behind Settings › Appearance.
func (a *App) GetEditorSettings() (EditorSettings, error) {
	return loadEditorSettings()
}

// SaveEditorSettings validates before persisting. The errors are shown
// verbatim in the modal.
func (a *App) SaveEditorSettings(s EditorSettings) error {
	s.Command = strings.TrimSpace(s.Command)
	s.App = strings.TrimSpace(s.App)
	if err := validateEditorSettings(s); err != nil {
		return err
	}
	return saveEditorSettings(s)
}

// shellArgv0 lists program names that would turn the editor command
// into "run this string for me", re-introducing the shell the argv
// split exists to avoid.
//
// This is defense in depth, not a trust boundary: whoever can write
// this setting can also write a custom agent's Cmd (see
// ListCustomAgents). It exists so an *accidental* `sh -c` template —
// or a single injected setting write — is not a working shell.
var shellArgv0 = map[string]bool{
	"sh": true, "bash": true, "zsh": true, "fish": true, "dash": true,
	"cmd": true, "cmd.exe": true, "powershell": true, "powershell.exe": true,
	"pwsh": true, "python": true, "python3": true, "perl": true, "ruby": true,
	"node": true, "osascript": true, "env": true, "open": true,
	"xdg-open": true, "explorer": true, "explorer.exe": true,
	"rundll32": true, "rundll32.exe": true, "mshta": true,
	"wscript": true, "cscript": true,
}

func validateEditorSettings(s EditorSettings) error {
	switch s.Kind {
	case editorNone, editorVSCode, editorCursor, editorZed, editorSublime:
		return nil
	case editorApp:
		if runtime.GOOS != "darwin" {
			return errors.New("opening in a named app is macOS-only; use a command instead")
		}
		if s.App == "" {
			return errors.New("name the application to open files with")
		}
		return nil
	case editorCommand:
		if s.Command == "" {
			return errors.New("enter a command, e.g. nvim-qt +{line} {file}")
		}
		argv, err := splitArgv(s.Command)
		if err != nil {
			return err
		}
		if !strings.Contains(s.Command, "{file}") {
			return errors.New("the command must contain {file}")
		}
		base := strings.ToLower(filepath.Base(argv[0]))
		if shellArgv0[base] {
			return fmt.Errorf("%s is a shell, not an editor — give the editor's own command", argv[0])
		}
		return nil
	default:
		return fmt.Errorf("unknown editor kind %q", s.Kind)
	}
}

// splitArgv tokenizes a command template the way a shell would split
// words, honouring single and double quotes — and then stops. It does
// not expand variables, globs, backticks or $(...), so nothing in a
// file name can introduce a new word: placeholders are substituted
// *after* this split.
func splitArgv(s string) ([]string, error) {
	var argv []string
	var cur strings.Builder
	var quote rune
	started := false
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
		case r == '\'' || r == '"':
			quote = r
			started = true
		case r == ' ' || r == '\t':
			if started {
				argv = append(argv, cur.String())
				cur.Reset()
				started = false
			}
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	if quote != 0 {
		return nil, errors.New("unbalanced quote in command")
	}
	if started {
		argv = append(argv, cur.String())
	}
	if len(argv) == 0 {
		return nil, errors.New("empty command")
	}
	return argv, nil
}

// cmdMetaChars are the characters cmd.exe re-interprets. Go runs a
// .bat/.cmd through cmd.exe and cannot escape these (the "BatBadBut"
// class of bug), so a file whose *name* contains one would be command
// injection on ⇧⌘-click. Editor shims on Windows are normally
// code.cmd / subl.cmd, so this is the common path, not a corner.
const cmdMetaChars = "&|<>^%!\"\r\n"

// editorArgv builds the command line for the configured editor.
// line and col are 0 when the click carried no position.
func editorArgv(s EditorSettings, file string, line, col int) ([]string, error) {
	pos := func(withCol string, withoutCol string) string {
		if line <= 0 {
			return file
		}
		if col > 0 {
			return withCol
		}
		return withoutCol
	}
	switch s.Kind {
	case editorNone:
		return nil, errNoEditorConfigured
	case editorVSCode, editorCursor:
		bin := map[string]string{editorVSCode: "code", editorCursor: "cursor"}[s.Kind]
		if line <= 0 {
			return []string{bin, file}, nil
		}
		return []string{bin, "-g", pos(
			fmt.Sprintf("%s:%d:%d", file, line, col),
			fmt.Sprintf("%s:%d", file, line),
		)}, nil
	case editorZed:
		return []string{"zed", pos(
			fmt.Sprintf("%s:%d:%d", file, line, col),
			fmt.Sprintf("%s:%d", file, line),
		)}, nil
	case editorSublime:
		return []string{"subl", pos(
			fmt.Sprintf("%s:%d:%d", file, line, col),
			fmt.Sprintf("%s:%d", file, line),
		)}, nil
	case editorApp:
		if s.App == "" {
			return nil, errNoEditorConfigured
		}
		return []string{"open", "-a", s.App, file}, nil
	case editorCommand:
		argv, err := splitArgv(s.Command)
		if err != nil {
			return nil, err
		}
		// Substitute after the split: a file name with spaces stays
		// one argument, and no file name can add another.
		l, c := line, col
		if l <= 0 {
			l = 1
		}
		if c <= 0 {
			c = 1
		}
		rep := strings.NewReplacer(
			"{file}", file,
			"{line}", strconv.Itoa(l),
			"{col}", strconv.Itoa(c),
		)
		for i := range argv {
			argv[i] = rep.Replace(argv[i])
		}
		return argv, nil
	default:
		return nil, fmt.Errorf("unknown editor kind %q", s.Kind)
	}
}

// runEditor launches the configured editor on file. isDir is true when
// the click was on a directory, which editors open as a workspace.
//
// It returns errNoEditorConfigured when there is nothing to run, and
// OpenFile then falls back to the OS default handler.
func runEditor(file string, line, col int, isDir bool) error {
	s, err := loadEditorSettings()
	if err != nil {
		return err
	}
	if isDir {
		// A directory has no line to jump to.
		line, col = 0, 0
	}
	argv, err := editorArgv(s, file, line, col)
	if err != nil {
		return err
	}
	bin, err := lookEditorPath(argv[0])
	if err != nil {
		return fmt.Errorf("%s not found — check the editor setting, or install its command-line launcher", argv[0])
	}
	if err := checkBatchSafety(bin, argv[1:]); err != nil {
		return err
	}
	return proc.Command(bin, argv[1:]...).Start()
}

// checkBatchSafety refuses to pass cmd.exe metacharacters to a .bat or
// .cmd shim. See cmdMetaChars.
func checkBatchSafety(bin string, args []string) error {
	ext := strings.ToLower(filepath.Ext(bin))
	if ext != ".bat" && ext != ".cmd" {
		return nil
	}
	for _, a := range args {
		if i := strings.IndexAny(a, cmdMetaChars); i >= 0 {
			return fmt.Errorf("refusing to pass %q to %s: the character %q is reinterpreted by cmd.exe", a, filepath.Base(bin), string(a[i]))
		}
	}
	return nil
}
