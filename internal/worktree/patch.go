package worktree

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrPatchTooLarge is returned by DumpPatch when the recovery patch
// would exceed the caller's cap. The output file is not written.
var ErrPatchTooLarge = errors.New("worktree: recovery patch exceeds cap")

// BranchExists reports whether refs/heads/<branch> exists in repoDir.
// Exported wrapper over the internal probe so the registry can decide
// whether a deleted worktree is recreatable from its branch.
func BranchExists(repoDir, branch string) bool {
	if repoDir == "" || branch == "" {
		return false
	}
	return branchExists(context.Background(), repoDir, branch)
}

// DumpPatch writes a `git apply`-able patch of everything uncommitted
// in worktreePath to outPath: tracked modifications (`git diff HEAD`)
// plus every untracked, non-ignored file rendered as an addition.
//
// It exists for exactly one caller: the close path that deletes a
// worktree on the user's explicit instruction. That is the only place
// where closing a session destroys work, and a patch under the state
// dir is the difference between "unrecoverable" and "run git apply".
//
// Deliberately NOT `git stash`. The stash stack is shared by every
// worktree of a repository, so stashing here would push an entry onto
// a stack the user — or another Hive session, or an unrelated tool —
// can pop, reorder or drop. A plain file is inert.
//
// Nothing in the worktree is mutated: no `git add`, no index writes.
// The obvious one-subprocess trick (`git add -N .` so `git diff HEAD`
// picks up untracked files) is avoided for that reason — the dump runs
// before a delete that can still be refused upstream, and a read-only
// operation must stay read-only.
//
// Returns ErrPatchTooLarge (and writes nothing) when the accumulated
// patch would exceed capBytes; the caller records that fact rather
// than implying a patch exists. Accumulation stops as soon as the cap
// is passed, so a worktree full of large untracked blobs costs one
// oversized buffer, not a full traversal.
func DumpPatch(worktreePath, outPath string, capBytes int64) error {
	if worktreePath == "" {
		return errors.New("worktree.DumpPatch: empty worktree path")
	}
	var buf bytes.Buffer

	// Tracked changes, staged and unstaged, against HEAD. --binary so
	// a modified image or fixture round-trips through git apply.
	tracked, err := git(context.Background(), readTimeout, worktreePath,
		"diff", "--binary", "HEAD")
	if err != nil {
		return fmt.Errorf("git diff HEAD: %w", err)
	}
	buf.Write(tracked)
	if int64(buf.Len()) > capBytes {
		return ErrPatchTooLarge
	}

	// Untracked but not ignored. -z because a filename may contain a
	// newline, and this path must not silently drop such a file.
	out, err := git(context.Background(), readTimeout, worktreePath,
		"ls-files", "-o", "--exclude-standard", "-z")
	if err != nil {
		return fmt.Errorf("git ls-files -o: %w", err)
	}
	for name := range strings.SplitSeq(string(out), "\x00") {
		if name == "" {
			continue
		}
		d, derr := diffAgainstNull(worktreePath, name)
		if derr != nil {
			// One unreadable untracked file must not cost the user the
			// rest of the patch. Note it inline so the omission is
			// visible in the file itself, not only in the daemon log.
			fmt.Fprintf(&buf, "# hive: could not capture untracked file %q: %v\n", name, derr)
			continue
		}
		buf.Write(d)
		if int64(buf.Len()) > capBytes {
			return ErrPatchTooLarge
		}
	}

	if buf.Len() == 0 {
		// Nothing uncommitted. Writing an empty patch would advertise a
		// recovery that recovers nothing.
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outPath), 0o700); err != nil {
		return err
	}
	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, outPath)
}

// diffAgainstNull renders one untracked file as an addition diff.
//
// `git diff --no-index` exits 1 when the two inputs differ, which is
// the expected outcome for every call here — so exit 1 with output is
// success, and only a truly failed run (no output) is an error. That
// is also why this does not go through git(): that helper treats any
// non-zero exit as failure.
func diffAgainstNull(worktreePath, name string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), readTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", worktreePath,
		"diff", "--no-index", "--binary", "--", os.DevNull, name)
	cmd.Env = append(os.Environ(), "LC_ALL=C", "LANG=C")
	out, err := cmd.Output()
	if len(out) > 0 {
		return out, nil
	}
	if err != nil {
		return nil, err
	}
	return nil, nil
}
