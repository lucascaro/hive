package registry

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// #451: worktree setup failures are put to the user instead of
// silently branching from a stale ref (or silently dropping the
// worktree entirely). These tests drive real git, like the rest of the
// registry's worktree coverage.

// repoWithStaleOrigin builds a clone whose origin/main is populated and
// then made unreachable — the state a laptop is in off VPN, and the
// one that used to produce a worktree on stale code with nothing but a
// log line to show for it.
//
// Returns the local repo and the cached origin/main sha.
func repoWithStaleOrigin(t *testing.T) (repo, cachedTip string) {
	t.Helper()
	skipNonPosix(t)

	upstream := t.TempDir()
	runGit(t, upstream, "init", "-q", "--bare", "-b", "main")

	seed := t.TempDir()
	runGit(t, seed, "init", "-q", "-b", "main")
	runGit(t, seed, "-c", "user.email=t@t", "-c", "user.name=t",
		"commit", "--allow-empty", "-q", "-m", "seed")
	runGit(t, seed, "remote", "add", "origin", upstream)
	runGit(t, seed, "push", "-q", "origin", "main")

	parent := t.TempDir()
	repo = filepath.Join(parent, "repo")
	runGit(t, parent, "clone", "-q", upstream, repo)
	runGit(t, repo, "config", "user.email", "t@t")
	runGit(t, repo, "config", "user.name", "t")
	cachedTip = gitOutput(t, repo, "rev-parse", "HEAD")

	// Break the remote AFTER the clone, so origin/main stays cached.
	runGit(t, repo, "remote", "set-url", "origin", filepath.Join(t.TempDir(), "gone.git"))
	return repo, cachedTip
}

// parkedProject opens a registry over a project whose repo cannot
// reach its origin, and creates one worktree session — which parks.
func parkedProject(t *testing.T) (*Registry, *Entry, string) {
	t.Helper()
	repo, cachedTip := repoWithStaleOrigin(t)
	r := freshRegistry(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "stale", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create should park, not fail: %v", err)
	}
	_ = cachedTip
	return r, e, repo
}

func TestCreateParksOnFetchFailure(t *testing.T) {
	r, e, repo := parkedProject(t)

	info := r.Get(e.ID).Info()
	if info.PendingWorktreeChoice == nil {
		t.Fatal("a failed fetch must park the session on a user decision, not branch from the cached ref")
	}
	if got := info.PendingWorktreeChoice.Kind; got != wire.WorktreeChoiceFetchFailed {
		t.Errorf("Kind = %q, want %q", got, wire.WorktreeChoiceFetchFailed)
	}
	if info.Phase != wire.PhaseBlocked {
		t.Errorf("Phase = %q, want %q so the UI shows a decision, not a spinner", info.Phase, wire.PhaseBlocked)
	}
	if info.PendingWorktreeChoice.CachedRef != "origin/main" {
		t.Errorf("CachedRef = %q, want origin/main (the choice being offered)", info.PendingWorktreeChoice.CachedRef)
	}
	if info.PendingWorktreeChoice.Message == "" {
		t.Error("Message must carry git's own words; the user judges staleness from them")
	}
	// Nothing may exist yet: the decision comes first.
	if entries, _ := filepath.Glob(filepath.Join(repo, ".worktrees", "*")); len(entries) != 0 {
		t.Errorf("no worktree may exist while the question is unanswered; found %v", entries)
	}
	if e.Alive() {
		t.Error("a parked session has no process yet")
	}
}

func TestResolveWorktreeChoiceProceedUsesCachedRef(t *testing.T) {
	r, e, repo := parkedProject(t)
	cachedTip := gitOutput(t, repo, "rev-parse", "origin/main")

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed); err != nil {
		t.Fatalf("ResolveWorktreeChoice(proceed): %v", err)
	}
	defer r.Kill(e.ID, true)

	got := r.Get(e.ID)
	if got == nil || got.WorktreePath == "" {
		t.Fatal("proceeding must create the worktree from the cached ref")
	}
	if head := gitOutput(t, got.WorktreePath, "rev-parse", "HEAD"); head != cachedTip {
		t.Errorf("worktree HEAD = %s, want the cached origin/main %s", head, cachedTip)
	}
	if got.Info().PendingWorktreeChoice != nil {
		t.Error("the question must be cleared once answered, or a reconnecting client re-raises it")
	}
}

func TestResolveWorktreeChoiceRetrySucceeds(t *testing.T) {
	r, e, repo := parkedProject(t)

	// "Fix the network": point origin back at a reachable repo. The
	// retry must re-run the fetch — a retry that skipped it would
	// branch from the same stale ref it just warned about.
	good := t.TempDir()
	runGit(t, good, "init", "-q", "--bare", "-b", "main")
	runGit(t, repo, "remote", "set-url", "origin", good)
	runGit(t, repo, "push", "-q", "origin", "main")

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceRetry); err != nil {
		t.Fatalf("ResolveWorktreeChoice(retry): %v", err)
	}
	defer r.Kill(e.ID, true)

	got := r.Get(e.ID)
	if got == nil || got.WorktreePath == "" {
		t.Fatal("a retry against a reachable remote must produce the worktree")
	}
	if got.Info().PendingWorktreeChoice != nil {
		t.Error("a successful retry must clear the question")
	}
}

func TestResolveWorktreeChoiceCancelRemovesEntry(t *testing.T) {
	r, e, repo := parkedProject(t)

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceCancel); err != nil {
		t.Fatalf("ResolveWorktreeChoice(cancel): %v", err)
	}
	if got := r.Get(e.ID); got != nil {
		t.Error("cancel must leave no session behind")
	}
	if entries, _ := filepath.Glob(filepath.Join(repo, ".worktrees", "*")); len(entries) != 0 {
		t.Errorf("cancel must leave no worktree behind; found %v", entries)
	}
	if branches := gitOutput(t, repo, "branch", "--list"); strings.Contains(branches, "feature") {
		t.Errorf("cancel must leave no branch behind; got %q", branches)
	}
}

func TestKillWhileParkedCleansUp(t *testing.T) {
	r, e, _ := parkedProject(t)

	if err := r.Kill(e.ID, true); err != nil {
		t.Fatalf("Kill of a parked session: %v", err)
	}
	if got := r.Get(e.ID); got != nil {
		t.Error("killed session must be gone")
	}
	// The resume state must not outlive the entry: answering afterwards
	// would otherwise resurrect a create for a session the user closed.
	r.parkedMu.Lock()
	_, stillParked := r.parked[e.ID]
	r.parkedMu.Unlock()
	if stillParked {
		t.Error("kill must drop the parked create state, or it leaks for the daemon's lifetime")
	}
	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed); err != nil {
		t.Errorf("resolving a killed session must be a harmless no-op; got %v", err)
	}
	if got := r.Get(e.ID); got != nil {
		t.Error("resolving after a kill must not recreate the session")
	}
}

func TestCreateFailsWithNoControlClient(t *testing.T) {
	repo, _ := repoWithStaleOrigin(t)
	r := freshRegistry(t)
	// Nothing is attached that could render a dialog — a scripted
	// create, or the GUI having quit. Falling back to the cached ref
	// here is exactly the silent behaviour #451 removes, so the create
	// must fail instead.
	r.SetHasControlClient(func() bool { return false })

	p, err := r.CreateProject(wire.CreateProjectReq{Name: "stale", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
	})
	if err == nil {
		t.Fatal("create must fail when nothing can answer the worktree question")
	}
	if !strings.Contains(err.Error(), "no client") {
		t.Errorf("error should say why it failed; got %v", err)
	}
	if e != nil && r.Get(e.ID) != nil {
		t.Error("no entry may survive a create that could not be resolved")
	}
	if entries, _ := filepath.Glob(filepath.Join(repo, ".worktrees", "*")); len(entries) != 0 {
		t.Errorf("no worktree may be created on the failure path; found %v", entries)
	}
}

func TestResolveUnparkedSessionIsNoop(t *testing.T) {
	r := freshRegistry(t)
	if err := r.ResolveWorktreeChoice(context.Background(), "no-such-session", wire.WorktreeChoiceProceed); err != nil {
		t.Errorf("resolving an unknown session must be a no-op (clients race to answer); got %v", err)
	}

	// And the same for a real session that is not parked: the second of
	// two racing GUI windows must not see an error.
	r2, e, _ := parkedProject(t)
	if err := r2.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	defer r2.Kill(e.ID, true)
	if err := r2.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceCancel); err != nil {
		t.Errorf("second resolve must be a no-op, not an error; got %v", err)
	}
	if r2.Get(e.ID) == nil {
		t.Error("the losing resolve must not act: the session was already created")
	}
}

// TestParkedCreateDoesNotBlockOtherOperations is the load-bearing test
// for the whole design. The wait is indefinite, so parking must hold
// no goroutine and no gitMu — if it did, one user staring at a dialog
// would freeze every other create and kill in the daemon.
func TestParkedCreateDoesNotBlockOtherOperations(t *testing.T) {
	r, parked, _ := parkedProject(t)
	t.Cleanup(func() { _ = r.Kill(parked.ID, true) })

	// A second project on a healthy repo, created and killed while the
	// first session sits on its dialog.
	healthy := initGitRepo(t)
	p2, err := r.CreateProject(wire.CreateProjectReq{Name: "healthy", Cwd: healthy})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		e2, cerr := r.Create(context.Background(), wire.CreateSpec{
			ProjectID:   p2.ID,
			Shell:       "/bin/bash",
			UseWorktree: true,
		})
		if cerr != nil {
			done <- cerr
			return
		}
		done <- r.Kill(e2.ID, true)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("create+kill while another session is parked: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("a parked create blocked an unrelated create/kill: it is holding gitMu or a goroutine across the wait")
	}

	// The parked one is still parked and still answerable.
	if r.Get(parked.ID).Info().PendingWorktreeChoice == nil {
		t.Error("the parked session must still be waiting for its answer")
	}
}

func TestParkedEntryMarkedDeadOnRevive(t *testing.T) {
	repo, _ := repoWithStaleOrigin(t)
	stateDir := t.TempDir()
	t.Setenv("SHELL", "/bin/sh")

	r, err := Open(stateDir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "stale", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create should park: %v", err)
	}
	if r.Get(e.ID).Info().PendingWorktreeChoice == nil {
		t.Fatal("fixture: session did not park")
	}
	_ = r.Close()

	// Restart: the resume state died with the old daemon. Reviving this
	// entry as a plain session in the project directory is the silent
	// fallback #451 removes, so it must come back dead and say why.
	r2, err := Open(stateDir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	r2.MarkPendingRevive()

	got := r2.Get(e.ID)
	if got == nil {
		t.Fatal("the entry should still exist after a restart, so the user can see what happened")
	}
	if got.Phase == wire.PhaseReviving {
		t.Error("a parked entry must not be revived into a plain session")
	}
	if !strings.Contains(got.LastError, "interrupted") {
		t.Errorf("LastError should explain the interrupted worktree setup; got %q", got.LastError)
	}

	// And it must not repeat on every subsequent boot.
	_ = r2.Close()
	r3, err := Open(stateDir)
	if err != nil {
		t.Fatalf("third open: %v", err)
	}
	t.Cleanup(func() { _ = r3.Close() })
	if e3 := r3.Get(e.ID); e3 != nil && e3.awaitingChoice {
		t.Error("the marker must be cleared after the first boot handles it")
	}
}

func TestResolveClearsAwaitingMarker(t *testing.T) {
	r, e, _ := parkedProject(t)

	meta := filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json")
	before, err := os.ReadFile(meta)
	if err != nil {
		t.Fatalf("read session.json: %v", err)
	}
	if !strings.Contains(string(before), "awaiting_worktree_choice") {
		t.Fatal("a parked entry must persist the marker, or boot cannot tell it from an ordinary session")
	}

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed); err != nil {
		t.Fatalf("resolve: %v", err)
	}
	defer r.Kill(e.ID, true)

	after, err := os.ReadFile(meta)
	if err != nil {
		t.Fatalf("read session.json after resolve: %v", err)
	}
	if strings.Contains(string(after), "awaiting_worktree_choice") {
		t.Error("the marker must be cleared on resolve, or a later restart resurrects a dialog already answered")
	}
}

// Guard against the parked map being mutated from two goroutines
// without its own lock — the failure mode would be a torn map under
// the race detector rather than a wrong answer.
func TestParkedMapConcurrentResolves(t *testing.T) {
	r, e, _ := parkedProject(t)
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })

	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed)
		}()
	}
	wg.Wait()
}
