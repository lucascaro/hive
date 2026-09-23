package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// resolvedTempDir is t.TempDir() as the picker will report it: macOS
// hands out /var/... which resolves to /private/var/..., and a Windows
// runner's %TEMP% can be an 8.3 short name (RUNNER~1) that expands.
func resolvedTempDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	resolved, err := resolveDir(dir)
	if err != nil {
		t.Fatalf("resolveDir(%q): %v", dir, err)
	}
	return resolved
}

func TestPickDirectoryDefaultPrefersTheCallersDir(t *testing.T) {
	caller := resolvedTempDir(t)
	launch := resolvedTempDir(t)
	if got := pickDirectoryDefault(caller, launch); got != caller {
		t.Fatalf("got %q, want the caller's dir %q", got, caller)
	}
}

func TestPickDirectoryDefaultFallsBackToLaunchDirWhenTheCallersIsMissing(t *testing.T) {
	launch := resolvedTempDir(t)
	missing := filepath.Join(launch, "gone")
	if got := pickDirectoryDefault(missing, launch); got != launch {
		t.Fatalf("got %q, want the launch dir %q", got, launch)
	}
}

func TestPickDirectoryDefaultIsEmptyWhenNeitherExists(t *testing.T) {
	base := t.TempDir()
	a := filepath.Join(base, "a")
	b := filepath.Join(base, "b")
	if got := pickDirectoryDefault(a, b); got != "" {
		t.Fatalf("got %q, want \"\"", got)
	}
}

func TestPickDirectoryDefaultRejectsAFileForALaunchDir(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "f")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := pickDirectoryDefault("", file); got != "" {
		t.Fatalf("got %q, want \"\" for a launch dir that is a file", got)
	}
}

// Wails checks DefaultDirectory with os.Lstat, so a symlink (or, on
// Windows, a junction) to a directory is refused as "does not exist"
// even though every other consumer of the path follows it. Resolve it
// before handing it over.
func TestPickDirectoryDefaultResolvesASymlinkedDir(t *testing.T) {
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable here: %v", err)
	}
	got := pickDirectoryDefault(link, "")
	want, err := resolveDir(real)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %q, want the resolved target %q", got, want)
	}
}

// recordingOpener stands in for the Wails dialog: it logs the default
// directory each call was given and replays a scripted answer.
type recordingOpener struct {
	dirs    []string
	answers []func() (string, error)
}

func (r *recordingOpener) open(dir string) (string, error) {
	r.dirs = append(r.dirs, dir)
	i := len(r.dirs) - 1
	if i >= len(r.answers) {
		return "", errors.New("unexpected extra call")
	}
	return r.answers[i]()
}

func TestPickDirectoryWithRetriesWithoutADefaultWhenTheRuntimeRefusesIt(t *testing.T) {
	o := &recordingOpener{answers: []func() (string, error){
		func() (string, error) {
			return "", errors.New("default directory 'C:\\Users\\x\\git' does not exist")
		},
		func() (string, error) { return "/picked", nil },
	}}
	got, err := pickDirectoryWith(o.open, "C:\\Users\\x\\git")
	if err != nil || got != "/picked" {
		t.Fatalf("got (%q, %v), want (\"/picked\", nil)", got, err)
	}
	if len(o.dirs) != 2 || o.dirs[0] != "C:\\Users\\x\\git" || o.dirs[1] != "" {
		t.Fatalf("dialog opened with %q, want the default then no default", o.dirs)
	}
}

func TestPickDirectoryWithDoesNotRetryOtherErrors(t *testing.T) {
	boom := errors.New("selected directory does not exist")
	o := &recordingOpener{answers: []func() (string, error){
		func() (string, error) { return "", boom },
	}}
	if _, err := pickDirectoryWith(o.open, "/somewhere"); !errors.Is(err, boom) {
		t.Fatalf("err = %v, want the dialog's own error", err)
	}
	if len(o.dirs) != 1 {
		t.Fatalf("dialog opened %d times, want once", len(o.dirs))
	}
}

func TestPickDirectoryWithReturnsACancelAsIs(t *testing.T) {
	o := &recordingOpener{answers: []func() (string, error){
		func() (string, error) { return "", nil },
	}}
	got, err := pickDirectoryWith(o.open, "/somewhere")
	if err != nil || got != "" {
		t.Fatalf("got (%q, %v), want (\"\", nil)", got, err)
	}
	if len(o.dirs) != 1 {
		t.Fatalf("dialog opened %d times, want once", len(o.dirs))
	}
}

func TestPickDirectoryWithDoesNotRetryWhenThereWasNoDefault(t *testing.T) {
	refused := errors.New("default directory '' does not exist")
	o := &recordingOpener{answers: []func() (string, error){
		func() (string, error) { return "", refused },
	}}
	if _, err := pickDirectoryWith(o.open, ""); !errors.Is(err, refused) {
		t.Fatalf("err = %v, want the error passed through", err)
	}
	if len(o.dirs) != 1 {
		t.Fatalf("dialog opened %d times, want once", len(o.dirs))
	}
}
