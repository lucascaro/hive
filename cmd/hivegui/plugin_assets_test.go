package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The app's asset server falls through to the plugin handler, so a UI
// plugin's module is reachable at /plugins/<id>/ inside the webview.
func TestAppOptions_HandlerServesPluginAssets(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", state)
	dir := filepath.Join(state, "plugins", "notes")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ui.mjs"), []byte("export default 1"), 0o600); err != nil {
		t.Fatal(err)
	}
	opts := appOptions(&App{}, 800, 600)
	if opts.AssetServer == nil || opts.AssetServer.Handler == nil {
		t.Fatal("AssetServer.Handler is not set")
	}
	rec := httptest.NewRecorder()
	opts.AssetServer.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/plugins/notes/ui.mjs", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != "export default 1" {
		t.Fatalf("plugin asset: code %d body %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Fatalf("Content-Type %q", ct)
	}
}
