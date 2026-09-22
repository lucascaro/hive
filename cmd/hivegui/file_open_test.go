package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(base, "src", "foo.ts")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	homeFile := filepath.Join(home, "notes.md")
	if err := os.WriteFile(homeFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		base string
		in   string
		want string
		// wantErr alone would pass vacuously for the network paths: they
		// do not exist locally either, so a missing guard still errors —
		// after the Lstat that is the whole problem. wantRefused demands
		// the refusal come from isNetworkOrDevicePath.
		wantErr     bool
		wantRefused bool
	}{
		{"relative", base, "src/foo.ts", file, false, false},
		{"dot relative", base, "./src/foo.ts", file, false, false},
		{"parent traversal", filepath.Join(base, "src"), "../src/foo.ts", file, false, false},
		{"absolute ignores base", t.TempDir(), file, file, false, false},
		{"tilde", base, "~/notes.md", homeFile, false, false},
		{"surrounding space", base, "  src/foo.ts  ", file, false, false},
		{"missing file", base, "src/nope.ts", "", true, false},
		{"empty", base, "", "", true, false},
		{"relative with no base", "", "src/foo.ts", "", true, false},
		// A UNC path is stat'd on *hover*, and on Windows that dials the
		// host and authenticates — the user's NTLM hash leaves the
		// machine with no click at all.
		{"unc path", base, `\\evil.example.com\share\a.txt`, "", true, true},
		{"unc path with forward slashes", base, "//evil.example.com/share/a.txt", "", true, true},
		{"win32 device namespace", base, `\\?\C:\x`, "", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolvePath(tc.base, tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("resolvePath(%q, %q) = %q, want error", tc.base, tc.in, got)
				}
				if tc.wantRefused && !strings.Contains(err.Error(), "network or device path") {
					t.Fatalf("resolvePath(%q, %q) errored with %v — want the guard's refusal, not a failed stat", tc.base, tc.in, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("resolvePath(%q, %q): %v", tc.base, tc.in, err)
			}
			if got != tc.want {
				t.Fatalf("resolvePath(%q, %q) = %q, want %q", tc.base, tc.in, got, tc.want)
			}
		})
	}
}

// openRecorder swaps out every side effect so a dispatch test can
// assert what *would* have been launched.
type openRecorder struct {
	opened   string
	revealed string
	editor   string
	line     int
	col      int
	editErr  error
}

func (r *openRecorder) install(t *testing.T, goos string) {
	t.Helper()
	origOpen, origReveal, origEditor, origGoos := openDefaultFn, revealFn, runEditorFn, gooseFn
	openDefaultFn = func(p string) error { r.opened = p; return nil }
	revealFn = func(p string) error { r.revealed = p; return nil }
	runEditorFn = func(p string, line, col int, _ fileMeta, _ string) error {
		if r.editErr != nil {
			return r.editErr
		}
		r.editor, r.line, r.col = p, line, col
		return nil
	}
	gooseFn = func() string { return goos }
	t.Cleanup(func() {
		openDefaultFn, revealFn, runEditorFn, gooseFn = origOpen, origReveal, origEditor, origGoos
	})
}

func writeModeFile(t *testing.T, dir, name string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

// TestOpenFileDispatch is the criterion-6 test: a launchable file must
// be revealed, never opened, on every platform.
func TestOpenFileDispatch(t *testing.T) {
	app := &App{}

	t.Run("regular file opens", func(t *testing.T) {
		base := t.TempDir()
		writeModeFile(t, base, "notes.md", 0o644)
		var r openRecorder
		r.install(t, "darwin")
		if err := app.OpenFile(base, "notes.md", 0, 0, false); err != nil {
			t.Fatal(err)
		}
		if r.opened == "" || r.revealed != "" {
			t.Fatalf("opened=%q revealed=%q, want open only", r.opened, r.revealed)
		}
	})

	t.Run("directory is revealed", func(t *testing.T) {
		base := t.TempDir()
		if err := os.Mkdir(filepath.Join(base, "src"), 0o755); err != nil {
			t.Fatal(err)
		}
		var r openRecorder
		r.install(t, "darwin")
		if err := app.OpenFile(base, "src", 0, 0, false); err != nil {
			t.Fatal(err)
		}
		if r.revealed == "" || r.opened != "" {
			t.Fatalf("opened=%q revealed=%q, want reveal only", r.opened, r.revealed)
		}
	})

	t.Run("executable is revealed never opened", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("no exec bit on Windows; the .exe extension is covered by TestIsLaunchable")
		}
		base := t.TempDir()
		writeModeFile(t, base, "deploy.sh", 0o755)
		var r openRecorder
		r.install(t, "darwin")
		if err := app.OpenFile(base, "deploy.sh", 0, 0, false); err != nil {
			t.Fatal(err)
		}
		if r.opened != "" {
			t.Fatalf("opened %q — an executable must never be launched", r.opened)
		}
		if r.revealed == "" {
			t.Fatal("want reveal")
		}
	})

	// The guard resolvePath applies to the printed candidate has to
	// survive symlink resolution: EvalSymlinks itself would dial the
	// host named by the link target.
	t.Run("a symlink to a UNC path is refused before resolution", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privilege on Windows")
		}
		base := t.TempDir()
		link := filepath.Join(base, "notes.md")
		if err := os.Symlink(`\\evil.example.com\share\a.txt`, link); err != nil {
			t.Fatal(err)
		}
		var r openRecorder
		r.install(t, "windows")
		err := app.OpenFile(base, "notes.md", 0, 0, false)
		if err == nil || !strings.Contains(err.Error(), "network or device path") {
			t.Fatalf("err = %v — want the symlink target refused", err)
		}
		if r.opened != "" || r.revealed != "" {
			t.Fatalf("acted on a network symlink: %+v", r)
		}
	})

	t.Run("a UNC path never reaches the filesystem", func(t *testing.T) {
		var r openRecorder
		r.install(t, "windows")
		err := app.OpenFile(t.TempDir(), `\\evil.example.com\share\a.txt`, 0, 0, false)
		if err == nil || !strings.Contains(err.Error(), "network or device path") {
			t.Fatalf("err = %v — want the guard's refusal before any stat", err)
		}
		if r.opened != "" || r.revealed != "" {
			t.Fatalf("touched the network path: %+v", r)
		}
	})

	t.Run("windows trailing-dot name is revealed", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows itself will not create this name")
		}
		base := t.TempDir()
		writeModeFile(t, base, "evil.exe.", 0o644)
		var r openRecorder
		r.install(t, "windows")
		if err := app.OpenFile(base, "evil.exe.", 0, 0, false); err != nil {
			t.Fatal(err)
		}
		if r.opened != "" {
			t.Fatalf("opened %q — Win32 strips the trailing dot and would run evil.exe", r.opened)
		}
		if r.revealed == "" {
			t.Fatal("want reveal")
		}
	})

	t.Run("symlink to an executable is revealed", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("symlinks need privilege on Windows")
		}
		base := t.TempDir()
		target := writeModeFile(t, base, "real.command", 0o644)
		link := filepath.Join(base, "notes.md")
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
		var r openRecorder
		r.install(t, "darwin")
		if err := app.OpenFile(base, "notes.md", 0, 0, false); err != nil {
			t.Fatal(err)
		}
		if r.opened != "" {
			t.Fatalf("opened %q — the symlink target is a .command file", r.opened)
		}
	})

	t.Run("editor mode uses the editor with line and col", func(t *testing.T) {
		base := t.TempDir()
		writeModeFile(t, base, "src/foo.ts", 0o644)
		var r openRecorder
		r.install(t, "darwin")
		if err := app.OpenFile(base, "src/foo.ts", 12, 5, true); err != nil {
			t.Fatal(err)
		}
		if r.editor == "" || r.line != 12 || r.col != 5 {
			t.Fatalf("editor=%q line=%d col=%d", r.editor, r.line, r.col)
		}
		if r.opened != "" || r.revealed != "" {
			t.Fatalf("also opened=%q revealed=%q", r.opened, r.revealed)
		}
	})

	t.Run("editor mode with no editor falls back to the OS", func(t *testing.T) {
		base := t.TempDir()
		writeModeFile(t, base, "notes.md", 0o644)
		var r openRecorder
		r.editErr = errNoEditorConfigured
		r.install(t, "darwin")
		if err := app.OpenFile(base, "notes.md", 3, 0, true); err != nil {
			t.Fatal(err)
		}
		if r.opened == "" {
			t.Fatal("want the OS default handler as the fallback")
		}
	})

	t.Run("editor mode does not fall back on a real error", func(t *testing.T) {
		base := t.TempDir()
		writeModeFile(t, base, "notes.md", 0o644)
		var r openRecorder
		r.editErr = errors.New("code not found")
		r.install(t, "darwin")
		err := app.OpenFile(base, "notes.md", 0, 0, true)
		if err == nil {
			t.Fatal("want the editor error surfaced")
		}
		if r.opened != "" {
			t.Fatalf("opened %q — a failed editor launch must not silently open something else", r.opened)
		}
	})

	t.Run("missing file errors and launches nothing", func(t *testing.T) {
		base := t.TempDir()
		var r openRecorder
		r.install(t, "darwin")
		err := app.OpenFile(base, "nope.md", 0, 0, false)
		if err == nil {
			t.Fatal("want an error")
		}
		if r.opened != "" || r.revealed != "" || r.editor != "" {
			t.Fatalf("launched something for a missing file: %+v", r)
		}
	})
}

// TestResolveFilePathsMarksMissing is what decides the hover
// underline: a path that does not exist must come back empty.
func TestResolveFilePaths(t *testing.T) {
	base := t.TempDir()
	writeModeFile(t, base, "src/foo.ts", 0o644)
	app := &App{}
	got := app.ResolveFilePaths(base, []string{"src/foo.ts", "src/missing.ts", ""})
	if got[0] == "" || !strings.HasSuffix(got[0], filepath.Join("src", "foo.ts")) {
		t.Fatalf("got[0] = %q, want the resolved path", got[0])
	}
	if got[1] != "" || got[2] != "" {
		t.Fatalf("got %q and %q, want empty for a missing path", got[1], got[2])
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want one entry per candidate", len(got))
	}
}
