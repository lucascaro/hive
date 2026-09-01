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

// The OFL requires the licence notice to travel with the redistributed font
// files. Nothing imports a licence, and anything under frontend/src only
// reaches dist/ if something imports it — so the two texts live in
// frontend/public/, which Vite copies into dist/ verbatim. This asserts that
// arrangement still holds: moving them "back next to the fonts" would drop
// them out of every shipped binary silently.
func TestEmbeddedAssetsIncludeFontLicences(t *testing.T) {
	want := map[string]bool{
		"LICENSE-IBMPlexSans.txt":   false,
		"LICENSE-JetBrainsMono.txt": false,
	}
	err := fs.WalkDir(assets, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if _, ok := want[path.Base(p)]; ok && !d.IsDir() {
			// A zero-byte or truncated copy satisfies the notice
			// requirement no better than a missing one.
			b, rerr := fs.ReadFile(assets, p)
			if rerr != nil {
				return rerr
			}
			if !strings.Contains(string(b), "SIL OPEN FONT LICENSE Version 1.1") {
				t.Errorf("%s does not contain the OFL 1.1 text", p)
			}
			want[path.Base(p)] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk embedded assets: %v", err)
	}
	for name, found := range want {
		if !found {
			t.Errorf("%s missing from the embedded assets; it must live in "+
				"cmd/hivegui/frontend/public/fonts/ so Vite copies it into dist/", name)
		}
	}
}
