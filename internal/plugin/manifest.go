// Package plugin installs and supervises headless plugins: user-installed
// programs that hived runs as child processes and that talk to it over
// the ordinary wire protocol, on a socket of their own.
//
// A plugin is a directory with a hive-plugin.json manifest. Its "main"
// entry is a command; its "ui" entry is an ES module the Hive app loads
// (a plugin may have either or both). The Manager starts the command with HIVE_SOCKET pointing at
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
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
	"unicode"

	"github.com/lucascaro/hive/internal/plugin/pluginassets"
)

// APIVersion is the plugin API this build implements. Until 1.0 the API
// is best-effort and may break on any release, so a manifest must name
// exactly this version to be loaded; after 1.0 compatibility follows
// semver on the major version.
const APIVersion = "0.2"

// ManifestFile is the manifest's file name at the root of a plugin dir.
const ManifestFile = "hive-plugin.json"

// Manifest is the parsed hive-plugin.json.
type Manifest struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	APIVersion  string `json:"api_version"`
	Description string `json:"description,omitempty"`
	Main        *Entry `json:"main,omitempty"`
	// UI is the plugin's part inside the Hive app: an ES module the app
	// imports, and an optional stylesheet. See docs/plugins.md.
	UI *UIEntry `json:"ui,omitempty"`
}

// UIEntry names the files, relative to the plugin dir, that the Hive app
// loads for a plugin. They are served read-only by pluginassets.
type UIEntry struct {
	Entry string `json:"entry"`
	Style string `json:"style,omitempty"`
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

// hasUnsafeText reports control characters (Cc, which includes U+0085),
// invisible formatting characters (Cf: bidi overrides and isolates,
// zero-width spaces and joiners, U+FEFF) and line/paragraph separators.
// These fields are shown to the user in the install trust prompt, where a
// newline could forge a "Runs:" line, an override could reorder the real
// one, and a zero-width character could make a name look like another's.
func hasUnsafeText(s string) bool {
	return strings.ContainsFunc(s, func(r rune) bool {
		return unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp)
	})
}

// Validate checks the manifest's shape. The API-version check comes last
// so a manifest with a structural problem reports that instead.
func (m Manifest) Validate() error {
	if !pluginassets.ValidID(m.ID) {
		return fmt.Errorf("plugin: id %q must be lowercase letters, digits and hyphens (max 63)", m.ID)
	}
	if strings.TrimSpace(m.Name) == "" {
		return errors.New("plugin: name is required")
	}
	for field, v := range map[string]string{"name": m.Name, "version": m.Version, "description": m.Description} {
		if hasUnsafeText(v) {
			return fmt.Errorf("plugin: %s contains control or invisible formatting characters", field)
		}
	}
	if m.Main == nil && m.UI == nil {
		return errors.New(`plugin: a plugin needs "main", "ui" or both`)
	}
	if m.Main != nil {
		if len(m.Main.Command) == 0 || strings.TrimSpace(m.Main.Command[0]) == "" {
			return errors.New(`plugin: "main.command" is required`)
		}
		for _, arg := range m.Main.Command {
			if hasUnsafeText(arg) {
				return errors.New(`plugin: "main.command" contains control or invisible formatting characters`)
			}
		}
	}
	if m.UI != nil {
		if err := checkAsset("ui.entry", m.UI.Entry, ".js", ".mjs"); err != nil {
			return err
		}
		if m.UI.Style != "" {
			if err := checkAsset("ui.style", m.UI.Style, ".css"); err != nil {
				return err
			}
		}
	}
	if m.APIVersion != APIVersion {
		return fmt.Errorf("%w: plugin targets %q, this Hive provides %q", ErrAPIVersion, m.APIVersion, APIVersion)
	}
	return nil
}

// checkAsset validates a UI file path: relative, slash-separated, inside
// the plugin dir, no scheme, and one of the allowed extensions. The app
// builds a URL from it, so anything else could load code from elsewhere.
func checkAsset(field, p string, exts ...string) error {
	if p == "" {
		return fmt.Errorf("plugin: %q is required", field)
	}
	if strings.Contains(p, ":") || strings.Contains(p, "\\") || hasUnsafeText(p) || !fs.ValidPath(p) {
		return fmt.Errorf("plugin: %q must be a relative path inside the plugin (got %q)", field, p)
	}
	for _, e := range exts {
		if path.Ext(p) == e {
			return nil
		}
	}
	return fmt.Errorf("plugin: %q must end in %s (got %q)", field, strings.Join(exts, " or "), p)
}
