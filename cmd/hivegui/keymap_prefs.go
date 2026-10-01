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
	"os"
	"path/filepath"

	"github.com/lucascaro/hive/internal/registry"
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
