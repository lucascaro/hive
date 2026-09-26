// Package plugin installs and supervises headless plugins: user-installed
// programs that hived runs as child processes and that talk to it over
// the ordinary wire protocol, on a socket of their own.
//
// A plugin is a directory with a hive-plugin.json manifest. Its "main"
// entry is a command; the Manager starts it with HIVE_SOCKET pointing at
// a per-spawn socket the daemon tags with the plugin's id, restarts it
// when it crashes, and gives up (status "failed") when it crashes too
// often. Plugins run with the user's full privileges — there is no
// sandbox — which is why an install always lands disabled and a client
// must ask the user before enabling it. See docs/plugins.md.
package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// APIVersion is the plugin API this build implements. Until 1.0 the API
// is best-effort and may break on any release, so a manifest must name
// exactly this version to be loaded; after 1.0 compatibility follows
// semver on the major version.
const APIVersion = "0.1"

// ManifestFile is the manifest's file name at the root of a plugin dir.
const ManifestFile = "hive-plugin.json"

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// Manifest is the parsed hive-plugin.json.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	APIVersion  string `json:"api_version"`
	Description string `json:"description,omitempty"`
	Main        *Entry `json:"main"`
	// UI is reserved for GUI-side plugin entry points. This build does
	// not load them, and refuses a manifest that declares one rather
	// than silently running half a plugin.
	UI json.RawMessage `json:"ui,omitempty"`
}

// Entry is one runnable entry point.
type Entry struct {
	Command []string `json:"command"`
}

// ErrAPIVersion marks a manifest written for a different plugin API. It
// is a refusal, not a malformed file: the plugin installs and shows as
// "refused" so the user can see why it will not run.
var ErrAPIVersion = errors.New("plugin: incompatible api_version")

// LoadManifest reads and validates dir/hive-plugin.json. A manifest that
// parses but targets another API version is returned together with an
// error wrapping ErrAPIVersion.
func LoadManifest(dir string) (Manifest, error) {
	var m Manifest
	b, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if err != nil {
		return m, fmt.Errorf("plugin: read %s: %w", ManifestFile, err)
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return m, fmt.Errorf("plugin: parse %s: %w", ManifestFile, err)
	}
	return m, m.Validate()
}

// Validate checks the manifest's shape. The API-version check comes last
// so a manifest with a structural problem reports that instead.
func (m Manifest) Validate() error {
	if !idPattern.MatchString(m.ID) {
		return fmt.Errorf("plugin: id %q must be lowercase letters, digits and hyphens (max 63)", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("plugin: name is required")
	}
	if len(m.UI) > 0 {
		return errors.New(`plugin: "ui" entry points are not supported by this version of Hive`)
	}
	if m.Main == nil || len(m.Main.Command) == 0 || strings.TrimSpace(m.Main.Command[0]) == "" {
		return errors.New(`plugin: "main.command" is required`)
	}
	if m.APIVersion != APIVersion {
		return fmt.Errorf("%w: plugin targets %q, this Hive provides %q", ErrAPIVersion, m.APIVersion, APIVersion)
	}
	return nil
}
