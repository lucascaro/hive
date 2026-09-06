package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// An app launched from Finder inherits only the system PATH from
// /etc/paths. That has no Homebrew arm64 prefix (/opt/homebrew/bin) and
// no Node version manager directory — fnm mints one per shell and nvm
// installs itself into a shell rc file — so ./build.sh dies with
// `exec: "npm": executable file not found in $PATH`. The fix is the one
// VS Code settled on: ask the user's own shell what its PATH is.
//
// See src/vs/platform/shell/node/shellEnv.ts in microsoft/vscode; the
// flag table, the output markers and the 10s bound below all come from
// what that code learned the hard way.

// loginEnvTimeout bounds the shell probe. An rc file that blocks on
// input must not hang the build button.
var loginEnvTimeout = 10 * time.Second

// resolveEnvSentinel is exported into the probe shell so an rc file can
// detect this non-interactive use and skip its expensive interactive
// setup. VS Code's equivalent is VSCODE_RESOLVING_ENVIRONMENT.
const resolveEnvSentinel = "HIVE_RESOLVING_ENVIRONMENT"

// probeArgvs returns the argv tails to try for shell, best first. The
// trailing element of each is the -c equivalent; the command string is
// appended to it.
//
// -i matters as much as -l: a login *non-interactive* zsh reads
// .zshenv and .zprofile but never .zshrc, which is exactly where nvm
// and fnm install themselves. tcsh rejects -l alongside -c outright.
// A shell that takes neither form (pwsh, say) fails both attempts and
// the caller keeps the PATH it already had.
func probeArgvs(shell string) [][]string {
	switch strings.TrimSuffix(filepath.Base(shell), ".exe") {
	case "tcsh", "csh":
		return [][]string{{"-ic"}, {"-c"}}
	default:
		return [][]string{{"-i", "-l", "-c"}, {"-l", "-c"}}
	}
}

// loginPATH returns the PATH of the user's login shell, or "" when it
// cannot be determined. Resolved once per process: it costs a whole
// shell startup, and it does not change while the app runs.
var loginPATH = sync.OnceValue(func() string {
	return resolveLoginPATH(os.Getenv("SHELL"))
})

// loginPATHFn is the seam envWithLoginPATH reads through, mirroring
// extractZipFn and friends in update_apply_darwin.go. The probe result
// is cached for the life of the process, so a test cannot steer it by
// setting SHELL — the resolution itself is covered against a fake shell
// by resolveLoginPATH's own tests.
var loginPATHFn = loginPATH

// resolveLoginPATH runs shell and returns the PATH it reports, or ""
// if the probe fails. Split from loginPATH so tests can drive it with
// a fake shell without fighting the process-wide cache.
func resolveLoginPATH(shell string) string {
	if shell == "" {
		return ""
	}
	// Markers, because rc files print: greetings, powerlevel10k's
	// instant prompt, corporate login banners. Scanning output for the
	// first PATH= line reads that noise as data; taking what lies
	// between two unguessable markers does not.
	mark, err := randomMark()
	if err != nil {
		log.Printf("hivegui: could not generate a shell-probe marker: %v", err)
		return ""
	}
	// env -0 (supported by macOS env) so a value containing a newline
	// cannot forge a PATH= entry. /usr/bin/printf rather than the
	// builtin: csh has no printf builtin.
	command := fmt.Sprintf("/usr/bin/printf %%s %s; /usr/bin/env -0; /usr/bin/printf %%s %s", mark, mark)

	var lastErr error
	for _, argv := range probeArgvs(shell) {
		ctx, cancel := context.WithTimeout(context.Background(), loginEnvTimeout)
		cmd := exec.CommandContext(ctx, shell, append(argv, command)...)
		cmd.Env = append(os.Environ(), resolveEnvSentinel+"=1")
		out, err := cmd.Output()
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		block, ok := betweenMarks(out, mark)
		if !ok {
			lastErr = fmt.Errorf("no marked env block in %d bytes of output", len(out))
			continue
		}
		if path := pathFromEnvBlock(block); path != "" {
			return path
		}
		lastErr = fmt.Errorf("marked env block has no PATH")
	}
	log.Printf("hivegui: could not read the login PATH from %s: %v", shell, lastErr)
	return ""
}

// betweenMarks returns what the shell printed between the two markers.
func betweenMarks(out []byte, mark string) ([]byte, bool) {
	m := []byte(mark)
	start := bytes.Index(out, m)
	if start < 0 {
		return nil, false
	}
	start += len(m)
	end := bytes.LastIndex(out, m)
	if end <= start {
		return nil, false
	}
	return out[start:end], true
}

// pathFromEnvBlock reads PATH out of a NUL-separated `env -0` dump.
func pathFromEnvBlock(block []byte) string {
	for _, entry := range bytes.Split(block, []byte{0}) {
		if v, ok := strings.CutPrefix(string(entry), "PATH="); ok {
			return v
		}
	}
	return ""
}

func randomMark() (string, error) {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// envWithLoginPATH replaces PATH in env with the login shell's. Only
// PATH: the rest of the login environment is the user's shell session,
// not this build's, and importing it would clobber the HIVE_* vars the
// app sets deliberately.
func envWithLoginPATH(env []string) []string {
	path := loginPATHFn()
	if path == "" {
		return env
	}
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, "PATH=") {
			out = append(out, kv)
		}
	}
	return append(out, "PATH="+path)
}

// buildTools are what ./build.sh cannot run without and what actually
// goes missing under a Finder-launched PATH. lipo, git and the rest
// live in /usr/bin, which is on every PATH there is. Var so a test
// driving runBuildScript with a stub script can waive the check.
var buildTools = []string{"go", "npm"}

// missingBuildTools names the build tools that do not resolve on path.
// Checked up front so a PATH that is still wrong fails with something
// the user can act on, rather than as `build.sh failed` forty lines
// into npm's output.
func missingBuildTools(path string) []string {
	var missing []string
	for _, tool := range buildTools {
		if !executableIn(path, tool) {
			missing = append(missing, tool)
		}
	}
	return missing
}

// executableIn reports whether name resolves to an executable file on
// the given PATH. exec.LookPath answers for the *current* process's
// PATH, which is the one we are replacing.
func executableIn(path, name string) bool {
	for _, dir := range filepath.SplitList(path) {
		if dir == "" {
			dir = "."
		}
		st, err := os.Stat(filepath.Join(dir, name))
		if err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return true
		}
	}
	return false
}

// pathOf returns the PATH entry of an environment slice.
func pathOf(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			return v
		}
	}
	return ""
}
