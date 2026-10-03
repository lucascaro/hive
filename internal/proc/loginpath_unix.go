//go:build !windows

package proc

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

// A process launched from Finder (the GUI, and the hived it starts)
// inherits only the system PATH from /etc/paths. That has no Homebrew
// arm64 prefix (/opt/homebrew/bin) and no Node version manager
// directory — fnm mints one per shell and nvm installs itself into a
// shell rc file — so `npm` and `npx` are not found. The fix is the one
// VS Code settled on: ask the user's own shell what its PATH is.
//
// See src/vs/platform/shell/node/shellEnv.ts in microsoft/vscode; the
// flag table, the output markers and the 10s bound below all come from
// what that code learned the hard way.

// loginEnvTimeout bounds the shell probe. An rc file that blocks on
// input must not hang the caller. Worst case is twice this: the
// context kills the shell after one interval, and WaitDelay below
// grants another before giving up on a grandchild holding the pipe.
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

// LoginPATH returns the PATH of the user's login shell, or "" when it
// cannot be determined. Resolved once per process: it costs a whole
// shell startup, and it does not change while the process runs. Never
// call it while holding a lock other work waits on — the first call
// can take up to twice loginEnvTimeout.
var LoginPATH = sync.OnceValue(func() string {
	return resolveLoginPATH(os.Getenv("SHELL"))
})

// resolveLoginPATH runs shell and returns the PATH it reports, or ""
// if the probe fails. Split from LoginPATH so tests can drive it with
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
		log.Printf("proc: could not generate a shell-probe marker: %v", err)
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
		// Killing the shell is not enough to make Output() return.
		// Output() waits for the stdout *pipe* to close, and a
		// grandchild inherits it — an rc file that backgrounds a
		// version-manager warmup or an update check holds the pipe open
		// for as long as that job runs, which is exactly the kind of
		// thing an interactive rc does. Without WaitDelay the context
		// bound above is decorative: a shell doing `sleep 30 &` returns
		// after 30 seconds against a 1-second context, with a nil error.
		cmd.WaitDelay = loginEnvTimeout
		out, err := cmd.Output()
		cancel()
		// Parse before judging err: what the shell printed is complete
		// and correct even when WaitDelay fired on a lingering
		// grandchild, and discarding it there would drop a perfectly
		// good PATH for the users most likely to need one.
		if block, ok := betweenMarks(out, mark); ok {
			if path := pathFromEnvBlock(block); path != "" {
				return path
			}
			lastErr = fmt.Errorf("marked env block has no PATH")
			continue
		}
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = fmt.Errorf("no marked env block in %d bytes of output", len(out))
	}
	log.Printf("proc: could not read the login PATH from %s: %v", shell, lastErr)
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

// LookPathIn returns the file name resolves to on the given PATH, or ""
// when it resolves to nothing. exec.LookPath answers for the *current*
// process's PATH, which is the one a login PATH replaces.
func LookPathIn(path, name string) string {
	for _, dir := range filepath.SplitList(path) {
		// An empty entry means the working directory. Skip it, as
		// exec.LookPath refuses such results (ErrDot): this picks the
		// binary that runs, and a checkout-relative binary must not win.
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err == nil && !st.IsDir() && st.Mode()&0o111 != 0 {
			return p
		}
	}
	return ""
}
