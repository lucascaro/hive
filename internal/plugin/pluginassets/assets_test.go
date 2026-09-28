package pluginassets

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func serve(t *testing.T, state, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	Handler(state).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
	return rec
}

func fixture(t *testing.T) string {
	t.Helper()
	state := t.TempDir()
	dir := filepath.Join(state, InstallDir, "notes")
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"ui.mjs": "export default () => ({})", "ui.css": ".notes-x{}", "sub/a.js": "1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A secret beside the install dir must never be reachable.
	if err := os.WriteFile(filepath.Join(state, "plugins.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return state
}

func TestAssetHandler_ServesModuleWithJSMime(t *testing.T) {
	state := fixture(t)
	for _, tc := range []struct{ path, ctype string }{
		{"/plugins/notes/ui.mjs", "text/javascript"},
		{"/plugins/notes/sub/a.js", "text/javascript"},
		{"/plugins/notes/ui.css", "text/css"},
	} {
		rec := serve(t, state, tc.path)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: code %d", tc.path, rec.Code)
		}
		if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, tc.ctype) {
			t.Errorf("%s: Content-Type %q, want %s", tc.path, got, tc.ctype)
		}
		if rec.Header().Get("Cache-Control") != "no-store" {
			t.Errorf("%s: missing no-store", tc.path)
		}
	}
}

func TestAssetHandler_RefusesTraversal(t *testing.T) {
	state := fixture(t)
	for _, p := range []string{
		"/plugins/notes/../../plugins.json",
		"/plugins/notes/..%2f..%2fplugins.json",
		"/plugins/../plugins.json",
		"/plugins/notes//etc/passwd",
	} {
		if rec := serve(t, state, p); rec.Code == http.StatusOK {
			t.Errorf("%s served: %q", p, rec.Body.String())
		}
	}
}

func TestAssetHandler_BadOrUnknownID404(t *testing.T) {
	state := fixture(t)
	for _, p := range []string{"/plugins/Notes/ui.mjs", "/plugins/nope/ui.mjs", "/plugins/-x/ui.mjs", "/plugins/notes", "/plugins/notes/", "/other/notes/ui.mjs"} {
		if rec := serve(t, state, p); rec.Code != http.StatusNotFound {
			t.Errorf("%s: code %d, want 404", p, rec.Code)
		}
	}
}

func TestAssetHandler_DirectoryIs404(t *testing.T) {
	state := fixture(t)
	if rec := serve(t, state, "/plugins/notes/sub"); rec.Code != http.StatusNotFound {
		t.Errorf("directory: code %d, want 404 (body %q)", rec.Code, rec.Body.String())
	}
}

func TestAssetHandler_RefusesWrites(t *testing.T) {
	state := fixture(t)
	rec := httptest.NewRecorder()
	Handler(state).ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/plugins/notes/ui.mjs", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST: code %d", rec.Code)
	}
}

func TestValidID(t *testing.T) {
	for id, want := range map[string]bool{"webhook": true, "plan-review": true, "a": true, "0x": true, "": false, "-a": false, "A": false, "a_b": false, strings.Repeat("a", 64): false} {
		if ValidID(id) != want {
			t.Errorf("ValidID(%q) = %v", id, !want)
		}
	}
}
