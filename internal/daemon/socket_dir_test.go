package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCheckSocketDirAcceptsOwn0700(t *testing.T) {
	skipOnWindows(t)
	dir := filepath.Join(t.TempDir(), "hive")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := CheckSocketDir(filepath.Join(dir, "hived.sock")); err != nil {
		t.Fatalf("CheckSocketDir: %v", err)
	}
}

func TestCheckSocketDirRejectsGroupOrWorldAccess(t *testing.T) {
	skipOnWindows(t)
	for _, mode := range []os.FileMode{0o755, 0o770, 0o707, 0o777} {
		dir := filepath.Join(t.TempDir(), "hive")
		if err := os.Mkdir(dir, mode); err != nil {
			t.Fatal(err)
		}
		// umask-proof: Mkdir's mode is masked, chmod's is not.
		if err := os.Chmod(dir, mode); err != nil {
			t.Fatal(err)
		}
		if err := CheckSocketDir(filepath.Join(dir, "hived.sock")); err == nil {
			t.Errorf("mode %o: want error, got nil", mode)
		}
	}
}

func TestCheckSocketDirRejectsSymlink(t *testing.T) {
	skipOnWindows(t)
	base := t.TempDir()
	real := filepath.Join(base, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "hive")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if err := CheckSocketDir(filepath.Join(link, "hived.sock")); err == nil {
		t.Fatal("symlinked socket dir accepted")
	}
}

func TestEnsureSocketDirCreates0700(t *testing.T) {
	skipOnWindows(t)
	sock := filepath.Join(t.TempDir(), "hive", "hived.sock")
	if err := EnsureSocketDir(sock); err != nil {
		t.Fatalf("EnsureSocketDir: %v", err)
	}
	st, err := os.Lstat(filepath.Dir(sock))
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o700 {
		t.Fatalf("mode = %o, want 700", st.Mode().Perm())
	}
}

// EnsureSocketDir must not paper over a pre-existing loose directory:
// MkdirAll is a no-op there, so the verify is the only thing standing
// between the daemon and a squatted path.
func TestEnsureSocketDirRejectsPreExistingLooseDir(t *testing.T) {
	skipOnWindows(t)
	dir := filepath.Join(t.TempDir(), "hive")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	if err := EnsureSocketDir(filepath.Join(dir, "hived.sock")); err == nil {
		t.Fatal("EnsureSocketDir accepted a world-writable pre-existing dir")
	}
}

func TestNewRefusesUnsafeSocketDir(t *testing.T) {
	skipOnWindows(t)
	tmp := shortTempDir(t)
	dir := filepath.Join(tmp, "open")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o777); err != nil {
		t.Fatal(err)
	}
	d, err := New(Config{SocketPath: filepath.Join(dir, "s"), StateDir: filepath.Join(tmp, "state")})
	if err == nil {
		_ = d.Close()
		t.Fatal("New accepted a world-writable socket dir")
	}
	if !strings.Contains(err.Error(), "socket dir") {
		t.Fatalf("New error = %v, want it to name the socket dir", err)
	}
}

// The default macOS path must not live in the shared /tmp, and must
// leave room under the 104-byte AF_UNIX cap once ".events" is appended.
func TestSocketPathDarwinIsPerUserTmp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin default")
	}
	t.Setenv("HIVE_SOCKET", "")
	// The fallback resolves through registry.StateDir(), so an
	// inherited HIVE_STATE_DIR (scripts/dev-iso.sh parks one under
	// /tmp) would otherwise decide this assertion.
	t.Setenv("HIVE_STATE_DIR", t.TempDir())
	got := SocketPath()
	if strings.HasPrefix(got, "/tmp/") || strings.HasPrefix(got, "/private/tmp/") {
		t.Fatalf("SocketPath() = %q; must not live under shared /tmp", got)
	}
	if n := len(EventSocketPath(got)); n > 100 {
		t.Fatalf("EventSocketPath(SocketPath()) is %d bytes; AF_UNIX limit on macOS is 104", n)
	}
}

// The Linux default follows XDG_RUNTIME_DIR, which the login manager
// already owns 0700, and never falls back to a shared /tmp.
func TestSocketPathLinuxUsesXDGRuntimeDir(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("linux default")
	}
	t.Setenv("HIVE_SOCKET", "")
	// Pin the fallback: without XDG_RUNTIME_DIR the path comes from
	// registry.StateDir(), which an inherited HIVE_STATE_DIR would
	// otherwise redirect (scripts/dev-iso.sh parks one under /tmp).
	state := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", state)
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/4242")
	if got, want := SocketPath(), "/run/user/4242/hive/hived.sock"; got != want {
		t.Fatalf("SocketPath() = %q, want %q", got, want)
	}
	t.Setenv("XDG_RUNTIME_DIR", "")
	if got, want := SocketPath(), filepath.Join(state, "hived.sock"); got != want {
		t.Fatalf("SocketPath() without XDG_RUNTIME_DIR = %q, want the state-dir fallback %q", got, want)
	}
}

// os.TempDir returns $TMPDIR verbatim, slash and all, so the /tmp guard
// has to compare a cleaned path. Without that, TMPDIR="/tmp/" puts the
// socket straight back into the shared directory this change exists to
// leave.
func TestSocketPathRejectsTrailingSlashTmp(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin uses $TMPDIR")
	}
	t.Setenv("HIVE_SOCKET", "")
	// t.TempDir() reads TMPDIR, so take it before the loop rewrites
	// it. Pinning HIVE_STATE_DIR is what makes the fallback assertion
	// about SocketPath rather than about the caller's environment.
	state := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", state)
	want := filepath.Join(state, "hived.sock")
	for _, tmp := range []string{"/tmp/", "/tmp//", "/private/tmp/"} {
		t.Setenv("TMPDIR", tmp)
		if got := SocketPath(); got != want {
			t.Errorf("TMPDIR=%q: SocketPath() = %q, want the state-dir fallback %q", tmp, got, want)
		}
	}
}
