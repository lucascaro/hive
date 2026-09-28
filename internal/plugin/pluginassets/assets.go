// Package pluginassets serves an installed plugin's UI files to the Hive
// app. It is a leaf on purpose: the GUI and hived-ws-bridge import it to
// load plugin UI, and must not link the plugin Manager (supervisor,
// runner, os/exec) to do so. The files are read-only here; the Manager in
// hived is the only writer of the plugin install dirs.
package pluginassets

import (
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// Prefix is the URL path every plugin asset is served under:
// /plugins/<id>/<path inside the installed plugin>.
const Prefix = "/plugins/"

// InstallDir is the directory under the state dir that holds installed
// plugins, one subdirectory per id. internal/plugin shares it.
const InstallDir = "plugins"

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidID reports whether id is a well-formed plugin id: lowercase
// letters, digits and hyphens, starting with a letter or digit, at most
// 63 characters. The manifest check and the asset URL both use it, so an
// id that installs is always one that can be served.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// Handler serves GET /plugins/<id>/<path> from stateDir/plugins/<id>.
// Directories, unknown ids, malformed ids and paths that escape the
// plugin dir are all 404 (or 400 from net/http for "..").
func Handler(stateDir string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		rest, ok := strings.CutPrefix(r.URL.Path, Prefix)
		if !ok {
			http.NotFound(w, r)
			return
		}
		id, name, _ := strings.Cut(rest, "/")
		if !ValidID(id) || name == "" || !fs.ValidPath(name) {
			http.NotFound(w, r)
			return
		}
		fsys := os.DirFS(filepath.Join(stateDir, InstallDir, id))
		st, err := fs.Stat(fsys, name)
		if err != nil || st.IsDir() {
			http.NotFound(w, r)
			return
		}
		// WKWebView refuses a module script served with a non-JavaScript
		// type, and mime.TypeByExtension(".mjs") is empty on some systems.
		switch path.Ext(name) {
		case ".js", ".mjs":
			w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		case ".css":
			w.Header().Set("Content-Type", "text/css; charset=utf-8")
		}
		// A reinstall replaces the files in place; never serve a stale copy.
		w.Header().Set("Cache-Control", "no-store")
		http.ServeFileFS(w, r, fsys, name)
	})
}
