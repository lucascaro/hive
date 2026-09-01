package main

import (
	"io/fs"
	"path"
	"strings"
	"testing"
)

// The brand presets are unusable without the bundled webfonts, and the failure
// mode is silent: the app falls back to a system font and only looks slightly
// wrong. Vite hashes the filenames, so match on extension, not name.
//
// This is also the cross-platform proof the fonts reach the Windows zip and
// the Linux build: no OS-specific packaging step touches frontend/dist, so a
// green run on each CI leg means that leg's binary carries them.
func TestEmbeddedAssetsIncludeWebfonts(t *testing.T) {
	var n int
	err := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.EqualFold(path.Ext(p), ".woff2") {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}
	if n < 6 {
		t.Fatalf("embedded .woff2 files = %d, want >= 6 (3 Plex Sans + 3 JetBrains Mono); "+
			"did `npm run build` run before `go test`?", n)
	}
}
