package plugin

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/lucascaro/hive/internal/proc"
)

// cloneTimeout bounds a git install. Var so tests can shrink it.
var cloneTimeout = 120 * time.Second

// scpLike matches git's scp-style remote, user@host:path.
var scpLike = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^/]`)

// isGitURL reports whether source names a remote for git rather than a
// local directory, and whether that remote's transport is allowed.
// Allowed: https, ssh, git, file, and scp-style ssh. Everything else a
// git URL can say — ext:: in particular, which runs an arbitrary
// command — is refused before git ever sees it.
func isGitURL(source string) (isURL bool, err error) {
	if scpLike.MatchString(source) {
		return true, nil
	}
	i := strings.Index(source, "://")
	if j := strings.Index(source, "::"); j >= 0 && (i < 0 || j < i) {
		return true, fmt.Errorf("plugin: git transport %q is not allowed", source[:j])
	}
	if i < 0 {
		return false, nil
	}
	switch strings.ToLower(source[:i]) {
	case "https", "ssh", "git", "file":
		return true, nil
	}
	return true, fmt.Errorf("plugin: git URL scheme %q is not allowed (use https, ssh, git or file)", source[:i])
}

// resolveLocal turns a user-typed local source into an absolute path.
// A relative path would resolve against the daemon's working directory,
// which the user never sees, so it is refused; ~/ is expanded.
func resolveLocal(source string) (string, error) {
	if rest, ok := strings.CutPrefix(source, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		source = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(source) {
		return "", fmt.Errorf("plugin: %q is neither an absolute path nor a git URL", source)
	}
	return filepath.Clean(source), nil
}

// copyTree copies src into dst (which must not exist), skipping .git
// and anything that is not a regular file or directory. Symlinks are
// skipped rather than followed: a plugin dir linking to ~/.ssh should
// not be copied into Hive's state dir.
func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == ".git" {
			return filepath.SkipDir
		}
		target := filepath.Join(dst, rel)
		switch {
		case d.IsDir():
			return os.MkdirAll(target, 0o700)
		case d.Type().IsRegular():
			info, err := d.Info()
			if err != nil {
				return err
			}
			return copyFile(path, target, info.Mode().Perm())
		default:
			return nil
		}
	})
}

func copyFile(src, dst string, perm fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm&0o700)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// gitClone shallow-clones url into dst. Every prompt git or ssh could
// raise is turned into a failure — the daemon has no terminal, so a
// prompt would otherwise just hang until the timeout — and the clone
// runs in its own process group so cancelling kills git-remote-* and
// ssh too, not only the git leader (a grandchild holding the output
// pipe would otherwise keep Wait blocked).
func gitClone(ctx context.Context, url, dst string) (commit string, err error) {
	ctx, cancel := context.WithTimeout(ctx, cloneTimeout)
	defer cancel()
	out, err := runGit(ctx, "", "-c", "protocol.ext.allow=never", "clone", "--depth", "1", "--quiet", "--", url, dst)
	if err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("plugin: git clone %s: %w", url, ctx.Err())
		}
		return "", fmt.Errorf("plugin: git clone %s: %v: %s", url, err, strings.TrimSpace(out))
	}
	out, err = runGit(ctx, dst, "rev-parse", "HEAD")
	if err != nil {
		return "", fmt.Errorf("plugin: git rev-parse: %v: %s", err, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := proc.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	ownGroup(cmd)
	cmd.Cancel = func() error { return killTree(cmd.Process) }
	cmd.WaitDelay = 5 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// gitEnv is the daemon's environment plus everything that turns a git
// or ssh prompt into a failure: the daemon has no terminal, so a prompt
// would otherwise hang the install until its timeout.
func gitEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_SSH_COMMAND=ssh -o BatchMode=yes",
		"LC_ALL=C",
	)
}

// errDuplicate is returned when an install names an id already present.
var errDuplicate = errors.New("already installed; remove it first to reinstall")
