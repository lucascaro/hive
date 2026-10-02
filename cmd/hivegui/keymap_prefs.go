// The user's keyboard shortcut overrides (spec 477), on disk.
//
// A GUI-owned JSON file under the state dir, like editor.json: it survives
// a GUI reload, a daemon restart and an app upgrade, and every window reads
// the same one. The frontend owns its meaning — chord grammar, conflict
// resolution, which commands exist (cmd/hivegui/frontend/src/lib/bindings.ts);
// Go only stores it.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lucascaro/hive/internal/registry"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Keymap is the on-disk shape of <stateDir>/keymap.json and the payload
// of the GetKeymap binding. Each half maps a command id to the chords that
// replace its defaults on that platform family; an empty list unbinds it.
type Keymap struct {
	Version int                 `json:"version,omitempty"`
	Mac     map[string][]string `json:"mac,omitempty"`
	Other   map[string][]string `json:"other,omitempty"`
}

func keymapPath() string {
	return filepath.Join(registry.StateDir(), "keymap.json")
}

// loadKeymap reads keymap.json. A missing file means "every default", not
// an error. A file that will not parse *is* an error, so a later save
// cannot overwrite a keymap the user hand-edited and mistyped — the same
// rule as editor.json.
func loadKeymap() (Keymap, error) {
	b, err := os.ReadFile(keymapPath())
	if err != nil {
		if os.IsNotExist(err) {
			return Keymap{}, nil
		}
		return Keymap{}, fmt.Errorf("read keymap.json: %w", err)
	}
	// PowerShell and Notepad write UTF-8 with a BOM and encoding/json
	// will not skip one; drop it rather than failing the load.
	b = bytes.TrimPrefix(b, []byte("\ufeff"))
	var k Keymap
	if err := json.Unmarshal(b, &k); err != nil {
		return Keymap{}, fmt.Errorf("parse %s: %w", keymapPath(), err)
	}
	return k, nil
}

// GetKeymap is the Wails binding the frontend loads its keymap from at
// boot.
func (a *App) GetKeymap() (Keymap, error) {
	return loadKeymap()
}

// SaveKeymap is the Wails binding behind Settings › Shortcuts. It writes
// the whole keymap, both halves: the tab edits only the current platform's
// half and hands the other back untouched, and overrides for plugins that
// are not loaded ride along the same way. Go does not validate it: the
// frontend owns the chord grammar (lib/keymap-edit.ts refuses bad keys
// before they reach the draft), and its resolver ignores, with a warning,
// any chord in a hand-edited file it cannot parse.
func (a *App) SaveKeymap(k Keymap) error {
	k.Version = 1
	return writeStateJSON("keymap.json", k)
}

// jsonFilter limits the export and import dialogs to JSON files.
var jsonFilter = []wruntime.FileFilter{{DisplayName: "Keymap (*.json)", Pattern: "*.json"}}

// ExportKeymap is the Wails binding behind Settings › Shortcuts › Export…:
// it asks where to save and writes k there, in keymap.json's own format.
// k is the tab's draft, both halves. False, with no error, when the user
// cancels the dialog.
func (a *App) ExportKeymap(k Keymap) (bool, error) {
	return exportKeymapWith(func() (string, error) {
		return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
			Title:           "Export shortcuts",
			DefaultFilename: "hive-keymap.json",
			Filters:         jsonFilter,
		})
	}, k)
}

func exportKeymapWith(save func() (string, error), k Keymap) (bool, error) {
	path, err := save()
	if err != nil || path == "" {
		return false, err
	}
	k.Version = 1
	// A file the user keeps and shares, not private state: readable like
	// any other document they save.
	if err := writeJSONFile(path, k, 0o644); err != nil {
		return false, err
	}
	return true, nil
}

// maxKeymapFile caps what PickKeymapFile reads: a keymap is a few KB, and
// anything this large is the wrong file.
const maxKeymapFile = 1 << 20

// PickKeymapFile is the Wails binding behind Settings › Shortcuts ›
// Import…: it asks for a file and returns its text, BOM stripped, or ""
// when the user cancels. It does not parse it: the frontend previews the
// import (lib/keymap-import.ts), and nothing is applied before the user
// confirms.
func (a *App) PickKeymapFile() (string, error) {
	return pickKeymapFileWith(func() (string, error) {
		return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
			Title:   "Import shortcuts",
			Filters: jsonFilter,
		})
	})
}

func pickKeymapFileWith(open func() (string, error)) (string, error) {
	path, err := open()
	if err != nil || path == "" {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxKeymapFile+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxKeymapFile {
		return "", fmt.Errorf("%s is larger than 1 MB, so it is not a Hive keymap", filepath.Base(path))
	}
	b = bytes.TrimPrefix(b, []byte("\ufeff"))
	// "" means the user cancelled the dialog, so an empty file must not
	// come back as "": it would do nothing and say nothing.
	if len(bytes.TrimSpace(b)) == 0 {
		return "", fmt.Errorf("%s is empty", filepath.Base(path))
	}
	return string(b), nil
}
