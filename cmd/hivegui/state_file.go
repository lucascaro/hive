// GUI-owned JSON files under the state dir (editor.json, keymap.json).
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lucascaro/hive/internal/registry"
)

// writeStateJSON writes v as indented JSON to <stateDir>/<name>, atomically
// (temp file + rename), so a crash mid-write never leaves a half file that
// the next load would refuse.
func writeStateJSON(name string, v any) error {
	dir := registry.StateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp, err := os.CreateTemp(dir, strings.TrimSuffix(name, ".json")+"-*.json")
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
	if err := os.Rename(tmpName, filepath.Join(dir, name)); err != nil {
		return fmt.Errorf("rename %s: %w", name, err)
	}
	return nil
}
