package registry

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// ErrWorktreeInUse is returned when a worktree operation is refused
// because a live session is running inside it. Unlike ErrWorktreeDirty
// and ErrWorktreeUnpushed this refusal is NOT overridable by force —
// removing or moving a directory out from under a running shell breaks
// that session with no upside. Callers close the sessions first.
var ErrWorktreeInUse = errors.New("registry: worktree has live sessions")

// ErrWorktreeUnpushed is returned when a worktree holds committed work
// that is not reachable from its comparison base — or when no base
// resolved at all, which is treated identically. Overridable by force
// after the user confirms.
var ErrWorktreeUnpushed = errors.New("registry: worktree has unpushed commits")

// ErrNotAGitRepo is returned when a project's cwd is not inside a git
// repository, so it has no worktrees to manage.
var ErrNotAGitRepo = errors.New("registry: project is not a git repository")

// projectRoot resolves a project's git root, or an error. Takes r.mu
// only long enough to read the project's cwd.
func (r *Registry) projectRoot(projectID string) (string, error) {
	r.mu.Lock()
	p, ok := r.projects[projectID]
	var cwd string
	if ok {
		cwd = p.Cwd
	}
	r.mu.Unlock()
	if !ok {
		return "", ErrProjectNotFound
	}
	if cwd == "" || !worktree.IsGitRepo(cwd) {
		return "", ErrNotAGitRepo
	}
	// MainRoot, not Root: when a project's cwd is itself a linked
	// worktree, Root reports that worktree's own top level — which
	// would put .worktrees/ in the wrong place, render the real
	// checkout as a removable row, and make managedPath refuse every
	// legitimate path.
	root, err := worktree.MainRoot(cwd)
	if err != nil {
		return "", ErrNotAGitRepo
	}
	return root, nil
}

// sessionsByWorktree snapshots which live sessions occupy which
// worktree path, keyed by the resolved path so it compares equal to
// what worktree.List reports (macOS /var vs /private/var).
func (r *Registry) sessionsByWorktree() map[string][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string][]string)
	for _, id := range r.order {
		e := r.entries[id]
		if e == nil || e.WorktreePath == "" {
			continue
		}
		key := worktree.ResolvePath(e.WorktreePath)
		out[key] = append(out[key], e.ID)
	}
	return out
}

// ListWorktrees returns the worktree inventory for one project: every
// registered worktree with its safety status and occupying sessions,
// plus the local branches that have no worktree at all (the orphaned
// branches a user can pick work back up from).
//
// A project whose cwd is not a git repo returns a response with an
// empty RepoRoot rather than an error — the browser says "not a git
// repository" instead of showing a failure.
func (r *Registry) ListWorktrees(projectID string) (wire.WorktreesResp, error) {
	resp := wire.WorktreesResp{ProjectID: projectID}
	root, err := r.projectRoot(projectID)
	if err != nil {
		if errors.Is(err, ErrNotAGitRepo) {
			return resp, nil
		}
		return resp, err
	}
	resp.RepoRoot = root

	// GitHub is asked in parallel with the git work below, not before
	// it: the call is a network round trip that has nothing to wait
	// for, and running it first made every cold open pay for it twice
	// over. It is also deliberately outside the git lock.
	started := time.Now()
	var (
		ghSet     ghMerged
		ghElapsed time.Duration
		ghDone    = make(chan struct{})
	)
	go func() {
		defer close(ghDone)
		ghSet = ghMergedLookup(root)
		ghElapsed = time.Since(started)
	}()

	r.gitMu.Lock()
	defer r.gitMu.Unlock()

	trees, err := worktree.List(root)
	if err != nil {
		return resp, fmt.Errorf("list worktrees: %w", err)
	}
	// The branch listing and the per-worktree probes below are
	// independent read-only git work, so they overlap too. Same
	// reasoning as the GitHub call: nothing here waits on anything
	// else, and a repo with hundreds of branches makes the difference
	// visible.
	branchesStart := time.Now()
	var (
		branches        []worktree.BranchInfo
		branchesErr     error
		branchesElapsed time.Duration
		branchesDone    = make(chan struct{})
	)
	go func() {
		defer close(branchesDone)
		branches, branchesErr = worktree.ListBranches(root)
		branchesElapsed = time.Since(branchesStart)
	}()
	claims := r.sessionsByWorktree()
	mainPath := worktree.ResolvePath(root)

	// Each row costs a handful of git subprocesses, so they are probed
	// concurrently — sequentially this is the slowest part of opening
	// the browser on a repo with several worktrees.
	inspectStart := time.Now()
	resp.Worktrees = make([]wire.WorktreeInfo, len(trees))
	// The overlay is read from here on, so this is where the GitHub
	// call is finally waited on — by now it has been running for the
	// length of the branch listing.
	<-ghDone
	var wg sync.WaitGroup
	for i, t := range trees {
		info := wire.WorktreeInfo{
			Path:       t.Path,
			Branch:     t.Branch,
			Detached:   t.Detached,
			IsMain:     t.Path == mainPath,
			SessionIDs: claims[t.Path],
		}
		// The main checkout is listed for context only — it is never
		// removable, so spending a status probe on it is waste.
		if info.IsMain {
			resp.Worktrees[i] = info
			continue
		}
		wg.Add(1)
		go func(i int, t worktree.Info, info wire.WorktreeInfo) {
			defer wg.Done()
			st, ierr := worktree.Inspect(root, t.Path)
			if ierr != nil {
				// Unknown is the safe reading: an unprobeable
				// worktree must not render as disposable.
				log.Printf("registry: inspect worktree %s: %v", t.Path, ierr)
				info.Unknown = true
			} else {
				info.Uncommitted, info.Unpushed, info.Unknown = st.Uncommitted, st.Unpushed, st.Unknown
				info.Upstream = st.Upstream
				info.Merged = st.Merged || ghConfirms(root, t.Branch, ghSet)
			}
			resp.Worktrees[i] = info
		}(i, t, info)
	}
	wg.Wait()
	inspectElapsed := time.Since(inspectStart)

	<-branchesDone
	if branchesErr != nil {
		return resp, fmt.Errorf("list branches: %w", branchesErr)
	}
	// The branch listing already carries every local branch's tip
	// subject, worktree or not, so the worktree rows get theirs from
	// here rather than paying another git spawn each.
	subjects := make(map[string]string, len(branches))
	for _, b := range branches {
		subjects[b.Name] = b.Subject
		if b.HasWorktree {
			continue
		}
		resp.OrphanBranches = append(resp.OrphanBranches, wire.BranchInfo{
			Name:     b.Name,
			Upstream: b.Upstream,
			Ahead:    b.Ahead,
			Merged:   b.Merged || ghConfirms(root, b.Name, ghSet),
			Subject:  b.Subject,
		})
	}
	for i := range resp.Worktrees {
		resp.Worktrees[i].Subject = subjects[resp.Worktrees[i].Branch]
	}
	sort.Slice(resp.OrphanBranches, func(i, j int) bool {
		return resp.OrphanBranches[i].Name < resp.OrphanBranches[j].Name
	})
	log.Printf("registry: ListWorktrees %s: %d worktrees, %d orphan branches in %s (gh=%s branches=%s inspect=%s)",
		projectID, len(resp.Worktrees), len(resp.OrphanBranches),
		time.Since(started).Round(time.Millisecond),
		ghElapsed.Round(time.Millisecond),
		branchesElapsed.Round(time.Millisecond),
		inspectElapsed.Round(time.Millisecond))
	return resp, nil
}

// managedPath validates that path is a worktree hive may act on: it
// must sit under <root>/.worktrees/ and must not be the repo root
// itself. Every mutation goes through this before touching disk — the
// request carries a client-supplied path, and a project cwd repointed
// at another repo must not turn into "delete any directory".
func managedPath(root, path string) (string, error) {
	if path == "" {
		return "", errors.New("registry: empty worktree path")
	}
	if !worktree.IsManaged(root, path) {
		return "", fmt.Errorf("registry: %s is not a hive-managed worktree of %s", path, root)
	}
	return worktree.ResolvePath(path), nil
}

// liveSessionsIn returns the ids of live sessions occupying path.
func (r *Registry) liveSessionsIn(path string) []string {
	return r.sessionsByWorktree()[worktree.ResolvePath(path)]
}

// RemoveWorktree deletes a worktree directory and prunes git's admin
// state. Refusals, in order:
//
//	ErrWorktreeInUse    — a live session is inside it (force cannot override)
//	ErrWorktreeDirty    — uncommitted changes (force overrides)
//	ErrWorktreeUnpushed — unpushed commits, or an unknown base (force overrides)
//
// deleteBranch additionally removes the branch the worktree was on.
// It defaults off because `git worktree remove` deliberately leaves
// the ref behind, and that ref is the user's last handle on the work.
func (r *Registry) RemoveWorktree(projectID, path string, force, deleteBranch, deleteRemote bool) error {
	root, err := r.projectRoot(projectID)
	if err != nil {
		return err
	}
	resolved, err := managedPath(root, path)
	if err != nil {
		return err
	}
	// The remote copy is the last handle on the work once the local ref
	// is gone, so the two go together. Asking for one without the other
	// used to be silently ignored.
	if deleteRemote && !deleteBranch {
		return errors.New("registry: delete_remote requires delete_branch")
	}
	if ids := r.liveSessionsIn(resolved); len(ids) > 0 {
		return fmt.Errorf("%w: %s", ErrWorktreeInUse, strings.Join(ids, ", "))
	}

	// Network call, so it happens before the git lock is taken.
	ghSet := ghMergedLookup(root)

	r.gitMu.Lock()
	defer r.gitMu.Unlock()

	// Re-check under the git lock. Creates and removes both run off the
	// daemon's read loop, so a session can start occupying this
	// worktree between the check above and the removal below — which
	// would leave a live shell in a directory that no longer exists.
	if ids := r.liveSessionsIn(resolved); len(ids) > 0 {
		return fmt.Errorf("%w: %s", ErrWorktreeInUse, strings.Join(ids, ", "))
	}

	st, err := worktree.Inspect(root, resolved)
	if err != nil {
		return fmt.Errorf("inspect worktree: %w", err)
	}
	// Commits already merged into the default ref (squash included)
	// are not lost by removing the worktree, so they do not need the
	// force path. Same verdict the client rendered, so the two agree.
	merged := st.Merged || ghConfirms(root, st.Branch, ghSet)
	if !force {
		if st.Uncommitted {
			return ErrWorktreeDirty
		}
		if (st.Unpushed > 0 && !merged) || st.Unknown {
			return ErrWorktreeUnpushed
		}
	}
	// Branch name comes from Inspect (the worktree's own HEAD), never
	// from the caller — a client must not be able to name one worktree
	// and have a different branch deleted.
	branch := st.Branch
	if err := worktree.Cleanup(root, resolved); err != nil {
		return fmt.Errorf("remove worktree: %w", err)
	}
	if deleteBranch && branch != "" {
		// force mirrors the user's confirmed intent: without it git
		// refuses to delete a branch holding unmerged commits, which
		// would leave the ref behind after the user explicitly asked
		// for it to go. `|| merged` covers the squash case: git's own
		// -d test is reachability, so a branch whose work landed as a
		// squash still needs -D even though nothing is lost.
		// The upstream is read before the local ref goes: once the
		// branch is deleted git can no longer say what it tracked.
		remote, remoteBranch := "", ""
		if deleteRemote {
			remote, remoteBranch = worktree.UpstreamOf(root, branch)
		}
		if err := worktree.DeleteBranch(root, branch, force || merged); err != nil {
			return fmt.Errorf("worktree removed, but deleting branch %s failed: %w", branch, err)
		}
		if remote != "" {
			if err := worktree.DeleteRemoteBranch(root, remote, remoteBranch); err != nil {
				return fmt.Errorf("worktree and local branch removed, but deleting %s/%s failed: %w",
					remote, remoteBranch, err)
			}
			log.Printf("registry: deleted remote branch %s/%s", remote, remoteBranch)
		}
	}
	log.Printf("registry: removed worktree %s (branch=%s deleteBranch=%v force=%v)", resolved, branch, deleteBranch, force)
	return nil
}

// CreateWorktreeForBranch materializes a worktree for a branch,
// returning its path. The branch normally already exists (the
// orphaned-branch case); worktree.CreateWorktree also handles a new
// name by branching from the upstream default ref, exactly as session
// creation does.
func (r *Registry) CreateWorktreeForBranch(ctx context.Context, projectID, branch string) (string, error) {
	if strings.TrimSpace(branch) == "" {
		return "", errors.New("registry: empty branch name")
	}
	root, err := r.projectRoot(projectID)
	if err != nil {
		return "", err
	}
	path := worktree.WorktreePath(root, branch)

	r.gitMu.Lock()
	defer r.gitMu.Unlock()

	if err := worktree.CreateWorktree(ctx, root, branch, path); err != nil {
		return "", err
	}
	worktree.EnsureGitignore(root)
	worktree.LinkAgentConfig(root, path)
	log.Printf("registry: created worktree %s for branch %s", path, branch)
	return path, nil
}

// ErrBranchUnmerged is returned when deleting a branch would discard
// commits that are not reachable from the repo's default ref.
// Overridable by force once the user has confirmed.
var ErrBranchUnmerged = errors.New("registry: branch has unmerged commits")

// ErrBranchHasWorktree is returned when a branch named for deletion
// still has a worktree checked out. Removing the ref while a directory
// is on it would leave the two disagreeing; the worktree goes first,
// through RemoveWorktree. Not overridable by force.
var ErrBranchHasWorktree = errors.New("registry: branch still has a worktree")

// DeleteBranch removes a local branch that has no worktree — the
// orphaned-branch half of the browser.
//
// Refuses a branch that still has a worktree (that is RemoveWorktree's
// job), and, without force, a branch holding commits not merged into
// the default ref. The unmerged check is git's own `branch -d`
// behaviour; it is probed first so the client gets a specific error
// code rather than a raw git message.
func (r *Registry) DeleteBranch(projectID, branch string, force, deleteRemote bool) error {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return errors.New("registry: empty branch name")
	}
	root, err := r.projectRoot(projectID)
	if err != nil {
		return err
	}

	ghSet := ghMergedLookup(root)

	r.gitMu.Lock()
	defer r.gitMu.Unlock()

	branches, err := worktree.ListBranches(root)
	if err != nil {
		return fmt.Errorf("list branches: %w", err)
	}
	var found *worktree.BranchInfo
	for i := range branches {
		if branches[i].Name == branch {
			found = &branches[i]
			break
		}
	}
	if found == nil {
		return fmt.Errorf("registry: no such branch %q", branch)
	}
	if found.HasWorktree {
		return fmt.Errorf("%w: %s", ErrBranchHasWorktree, branch)
	}
	// Merged covers squash merges, which git's own -d test cannot see;
	// such a branch is safe to delete but needs -D to actually go.
	merged := found.Merged || ghConfirms(root, branch, ghSet)
	if !force && !merged {
		return fmt.Errorf("%w: %s (%d ahead)", ErrBranchUnmerged, branch, found.Ahead)
	}
	remote, remoteBranch := "", ""
	if deleteRemote {
		remote, remoteBranch = worktree.UpstreamOf(root, branch)
	}
	if err := worktree.DeleteBranch(root, branch, force || merged); err != nil {
		return err
	}
	log.Printf("registry: deleted branch %s (force=%v)", branch, force)
	if remote != "" {
		if err := worktree.DeleteRemoteBranch(root, remote, remoteBranch); err != nil {
			return fmt.Errorf("local branch %s deleted, but deleting %s/%s failed: %w",
				branch, remote, remoteBranch, err)
		}
		log.Printf("registry: deleted remote branch %s/%s", remote, remoteBranch)
	}
	return nil
}

// RenameWorktree renames a worktree's branch and moves its directory
// to match — the two stay coupled, so the path on disk always agrees
// with the branch the UI shows.
//
// Refused while any session lives in the worktree: moving the
// directory would leave that session's shell in a cwd that no longer
// exists, which looks like a hang and loses the terminal's context.
func (r *Registry) RenameWorktree(projectID, path, newBranch string) error {
	newBranch = strings.TrimSpace(newBranch)
	if newBranch == "" {
		return errors.New("registry: empty branch name")
	}
	root, err := r.projectRoot(projectID)
	if err != nil {
		return err
	}
	resolved, err := managedPath(root, path)
	if err != nil {
		return err
	}
	if ids := r.liveSessionsIn(resolved); len(ids) > 0 {
		return fmt.Errorf("%w: %s", ErrWorktreeInUse, strings.Join(ids, ", "))
	}

	r.gitMu.Lock()
	defer r.gitMu.Unlock()

	// Same race as RemoveWorktree: moving the directory out from under
	// a session that started in the gap breaks that session's cwd.
	if ids := r.liveSessionsIn(resolved); len(ids) > 0 {
		return fmt.Errorf("%w: %s", ErrWorktreeInUse, strings.Join(ids, ", "))
	}

	st, err := worktree.Inspect(root, resolved)
	if err != nil {
		return fmt.Errorf("inspect worktree: %w", err)
	}
	if st.Branch == "" {
		return errors.New("registry: cannot rename a detached worktree")
	}
	if st.Branch == newBranch {
		return nil
	}
	dest := worktree.WorktreePath(root, newBranch)
	if err := worktree.RenameBranch(root, st.Branch, newBranch); err != nil {
		return err
	}
	if err := worktree.MoveWorktree(root, resolved, dest); err != nil {
		// Put the branch back so a failed move doesn't leave the ref
		// and the directory disagreeing — the state the whole
		// path/branch coupling exists to prevent.
		if rerr := worktree.RenameBranch(root, newBranch, st.Branch); rerr != nil {
			log.Printf("registry: rename rollback failed for %s: %v (branch is now %s, dir is still %s)",
				resolved, rerr, newBranch, resolved)
		}
		return err
	}
	log.Printf("registry: renamed worktree %s -> %s (branch %s -> %s)", resolved, dest, st.Branch, newBranch)
	return nil
}
