package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// On-disk layout, all under the daemon's state dir:
//
//	plugins.json            the installed set (this file)
//	plugins/<id>/           the installed copy; replaced on reinstall
//	plugin-data/<id>/       the plugin's own data: config.json, plugin.log;
//	                        survives remove and reinstall
const (
	storeFile  = "plugins.json"
	installDir = "plugins"
	dataDir    = "plugin-data"
)

// record is one installed plugin as persisted. The manifest itself is
// read from the install dir, so it is never duplicated here.
type record struct {
	ID      string `json:"id"`
	Source  string `json:"source"`
	Commit  string `json:"commit,omitempty"`
	Enabled bool   `json:"enabled"`
}

type storeDoc struct {
	Plugins []record `json:"plugins"`
}

func loadStore(stateDir string) ([]record, error) {
	b, err := os.ReadFile(filepath.Join(stateDir, storeFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc storeDoc
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("plugin: parse %s: %w", storeFile, err)
	}
	return doc.Plugins, nil
}

func saveStore(stateDir string, recs []record) error {
	b, err := json.MarshalIndent(storeDoc{Plugins: recs}, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(stateDir, storeFile, append(b, '\n'))
}

// writeFileAtomic writes blob to dir/name through a temp file in the
// same directory and a rename, so a crash never leaves a half-written
// plugins.json. Same discipline as internal/agent's persisted files.
func writeFileAtomic(dir, name string, blob []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, name+".*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the rename succeeds
	if _, err := tmp.Write(blob); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(dir, name))
}
