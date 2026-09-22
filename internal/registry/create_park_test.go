package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/session"
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

// Checking out a branch that already exists never consults upstream,
// so an unreachable origin must not park it, delay it, or fail it.
// This is the spec's non-goal, and it regressed once: the fetch was
// hoisted ahead of the branch probe, so an offline checkout parked
// behind a dialog whose text is not even true for a checkout.
func TestExistingBranchCheckoutNeverParksOnUnreachableOrigin(t *testing.T) {
	repo, _ := repoWithStaleOrigin(t)
	// A branch the user already has locally.
	runGit(t, repo, "branch", "existing-work")

	r := freshRegistry(t)
	// Nothing could answer a dialog, so a park here would FAIL the
	// create outright — which is exactly the user-visible regression.
	r.SetHasControlClient(func() bool { return false })
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "stale", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	start := time.Now()
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
		Branch:      "existing-work",
	})
	if err != nil {
		t.Fatalf("checking out an existing branch must not depend on origin: %v", err)
	}
	defer r.Kill(e.ID, true)

	if got := r.Get(e.ID); got == nil || got.WorktreePath == "" {
		t.Fatal("the checkout must produce a worktree")
	}
	if info := r.Get(e.ID).Info(); info.PendingWorktreeChoice != nil {
		t.Error("a checkout must never be parked on a fetch it does not depend on")
	}
	// The fetch has a 10s budget and this path must not pay it. The
	// bar sits just under that budget rather than at the milliseconds a
	// checkout really takes: the assertion is "it did not wait for the
	// fetch", and a tighter bound would flake on a loaded CI box. At
	// exactly 10s a fetch finishing just inside its budget would slip
	// through, so 9s keeps both the margin and the catch.
	if elapsed := time.Since(start); elapsed > 9*time.Second {
		t.Errorf("checkout waited %v — it paid for a fetch it should have skipped", elapsed)
	}
}

func TestResolveWorktreeChoiceProceedUsesCachedRef(t *testing.T) {
	r, e, repo := parkedProject(t)
	cachedTip := gitOutput(t, repo, "rev-parse", "origin/main")

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed, ""); err != nil {
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

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceRetry, ""); err != nil {
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
	// Read the real branch name off the question: the session name is
	// a random adjective-noun, so asserting on a guessed literal (an
	// earlier revision used "feature") can never fail.
	branch := ""
	if q := r.Get(e.ID).Info().PendingWorktreeChoice; q != nil {
		branch = q.Branch
	}

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceCancel, ""); err != nil {
		t.Fatalf("ResolveWorktreeChoice(cancel): %v", err)
	}
	if got := r.Get(e.ID); got != nil {
		t.Error("cancel must leave no session behind")
	}
	if entries, _ := filepath.Glob(filepath.Join(repo, ".worktrees", "*")); len(entries) != 0 {
		t.Errorf("cancel must leave no worktree behind; found %v", entries)
	}
	if branch == "" {
		t.Fatal("fixture: the parked question must name the branch, or the assertion below is vacuous")
	}
	if branches := gitOutput(t, repo, "branch", "--list"); strings.Contains(branches, branch) {
		t.Errorf("cancel must leave branch %q behind; got %q", branch, branches)
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
	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed, ""); err != nil {
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
	if err := r.ResolveWorktreeChoice(context.Background(), "no-such-session", wire.WorktreeChoiceProceed, ""); err != nil {
		t.Errorf("resolving an unknown session must be a no-op (clients race to answer); got %v", err)
	}

	// And the same for a real session that is not parked: the second of
	// two racing GUI windows must not see an error.
	r2, e, _ := parkedProject(t)
	if err := r2.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed, ""); err != nil {
		t.Fatalf("first resolve: %v", err)
	}
	defer r2.Kill(e.ID, true)
	if err := r2.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceCancel, ""); err != nil {
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
		t.Error("a parked entry must not be marked for revive")
	}

	// The phase alone is NOT enough, and this is the assertion that
	// matters: MarkPendingRevive leaves the entry in PhaseReady (what
	// clients render as dead), and ReviveWithPhase's first claim
	// accepts PhaseReady. Without the explicit decline, the boot pass
	// that runs right after would fork a plain session in the project
	// directory — reinstating the exact fallback this feature removes.
	revived, rerr := r2.ReviveWithPhase(e.ID, session.Options{
		Shell: "/bin/bash", Cols: 80, Rows: 24,
	})
	if revived {
		t.Error("the boot revive pass must not bring back a session that never completed worktree setup")
	}
	if !errors.Is(rerr, ErrNeverSpawn) {
		t.Errorf("want ErrNeverSpawn so the caller skips it without retrying; got %v", rerr)
	}
	if after := r2.Get(e.ID); after != nil && after.Alive() {
		t.Error("no PTY may be forked for a parked-then-interrupted entry")
	}
	if after := r2.Get(e.ID); after != nil && !strings.Contains(after.LastError, "interrupted") {
		t.Errorf("the revive attempt must not clobber the explanation; got %q", after.LastError)
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
	e3 := r3.Get(e.ID)
	if e3 == nil {
		t.Fatal("the entry must survive the second restart; a vanished entry would pass the marker check vacuously")
	}
	if e3.awaitingChoice {
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

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed, ""); err != nil {
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

// Exactly one of several racing resolves may act. The parked map is
// the arbiter, so this also exercises it from several goroutines —
// run the package with -race to get the torn-map failure too.
func TestParkedMapConcurrentResolves(t *testing.T) {
	r, e, repo := parkedProject(t)
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })

	var wg sync.WaitGroup
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed, "")
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("a losing resolve must be a no-op, not an error; got %v", err)
		}
	}

	// One winner: the session exists, with exactly one worktree.
	got := r.Get(e.ID)
	if got == nil || got.WorktreePath == "" {
		t.Fatal("one resolve must have created the session")
	}
	entries, _ := filepath.Glob(filepath.Join(repo, ".worktrees", "*"))
	if len(entries) != 1 {
		t.Errorf("want exactly one worktree from four racing resolves; got %v", entries)
	}
	r.parkedMu.Lock()
	leftover := len(r.parked)
	r.parkedMu.Unlock()
	if leftover != 0 {
		t.Errorf("parked map must be empty after the resolves; got %d", leftover)
	}
}

// An answer composed against a superseded question must not be applied
// under the new one's meaning: a session can park again with a
// different Kind, where "proceed" means something else entirely.
func TestStaleParkIDIsIgnored(t *testing.T) {
	r, e, _ := parkedProject(t)
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID,
		wire.WorktreeChoiceProceed, "a-park-that-is-no-longer-current"); err != nil {
		t.Fatalf("a stale answer is ignored, not an error; got %v", err)
	}
	info := r.Get(e.ID).Info()
	if info.PendingWorktreeChoice == nil {
		t.Fatal("the question must still stand after a stale answer")
	}
	// Answering the CURRENT park works.
	if err := r.ResolveWorktreeChoice(context.Background(), e.ID,
		wire.WorktreeChoiceProceed, info.PendingWorktreeChoice.ParkID); err != nil {
		t.Fatalf("answering the current park: %v", err)
	}
	if r.Get(e.ID).WorktreePath == "" {
		t.Error("the matching answer must be applied")
	}
}

// The data-loss case parking opened up. A parked create holds a
// PLANNED path indefinitely; ResolveBranchAndPath only avoids paths
// that exist on disk, so a later session can resolve to the same path
// and create it for real. Answering the stale question must not then
// delete that live session's worktree.
func TestCancelDoesNotDeleteAnotherSessionsWorktree(t *testing.T) {
	r, parked, repo := parkedProject(t)

	q := r.Get(parked.ID).Info().PendingWorktreeChoice
	if q == nil || q.Branch == "" {
		t.Fatal("fixture: the parked question must name a branch")
	}
	claimed := q.Branch

	// A second session materializes that exact path for real. Point
	// origin somewhere reachable so this one does not park too.
	good := t.TempDir()
	runGit(t, good, "init", "-q", "--bare", "-b", "main")
	runGit(t, repo, "remote", "set-url", "origin", good)
	runGit(t, repo, "push", "-q", "origin", "main")

	projects := r.ListProjects()
	if len(projects) == 0 {
		t.Fatal("fixture: no project")
	}
	live, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID:   projects[0].ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
		Branch:      claimed,
	})
	if err != nil {
		t.Fatalf("second create: %v", err)
	}
	defer r.Kill(live.ID, true)
	livePath := r.Get(live.ID).WorktreePath
	if livePath == "" {
		t.Fatal("the second session must have a real worktree")
	}
	// Something the user would lose.
	mustWriteFile(t, filepath.Join(livePath, "work.txt"), "uncommitted work\n")

	// Now answer the FIRST session's stale question with the
	// destructive choice.
	if err := r.ResolveWorktreeChoice(context.Background(), parked.ID,
		wire.WorktreeChoiceCancel, ""); err != nil {
		t.Fatalf("cancel: %v", err)
	}

	if _, err := os.Stat(filepath.Join(livePath, "work.txt")); err != nil {
		t.Fatalf("cancelling a parked create destroyed a LIVE session's worktree: %v", err)
	}
	if r.Get(live.ID) == nil {
		t.Error("the live session must survive")
	}
}

// The guard must not fail in the other direction. A `git worktree add`
// that fails after creating the directory (its 30s deadline SIGKILLs
// git, so git's own junk-cleanup never runs) still leaves debris —
// and that debris IS ours, because nothing occupied the path before we
// tried. Claiming ownership only on success would make Cancel leave a
// worktree behind, against the spec, and Retry fail forever.
func TestCancelCleansUpDebrisFromAFailedAdd(t *testing.T) {
	r, e, repo := parkedOnAddFailure(t)

	q := r.Get(e.ID).Info().PendingWorktreeChoice
	if q == nil {
		t.Fatal("fixture: the session must be parked on the add failure")
	}
	// Simulate the half-made directory a killed `git worktree add`
	// leaves behind at the planned path.
	wtDir := filepath.Join(repo, ".worktrees")
	if err := os.Chmod(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	debris := filepath.Join(wtDir, q.Branch)
	if err := os.MkdirAll(debris, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFile(t, filepath.Join(debris, "junk"), "half-made\n")

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID,
		wire.WorktreeChoiceCancel, ""); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	if r.Get(e.ID) != nil {
		t.Error("cancel must leave no session")
	}
	// "Picking Cancel leaves no session and no worktree" — the spec's
	// success criterion, which an unowned-debris guard would break.
	if _, err := os.Stat(debris); err == nil {
		t.Error("cancel left the failed add's directory behind; Retry would then fail forever")
	}
}

// The ownership guard has two clauses and they defend different
// things. Assert the live-entry clause on its own: a plan that DID
// create its path must still not delete it once a live session lives
// there (⌘P duplicate adopts a sibling's worktree).
func TestDiscardRefusesWhileALiveSessionLivesThere(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer r.Kill(e.ID, true)
	live := r.Get(e.ID)
	if live == nil || live.WorktreePath == "" {
		t.Fatal("fixture: no worktree")
	}
	mustWriteFile(t, filepath.Join(live.WorktreePath, "work.txt"), "keep me\n")

	// A plan that legitimately created this very path — the shape a
	// mid-create kill leaves behind — must still refuse, because an
	// entry is living there now.
	r.discardWorktree(createPlan{
		id:        "some-other-id",
		wtPath:    live.WorktreePath,
		wtBranch:  live.WorktreeBranch,
		wtCreated: true,
	})

	if _, err := os.Stat(filepath.Join(live.WorktreePath, "work.txt")); err != nil {
		t.Fatalf("discard deleted a live session's worktree: %v", err)
	}
}

// The park sink scrubs credentials and caps the message. Both are
// security/robustness properties of text that reaches a GUI dialog,
// hived.log, and every SessionInfo broadcast for the life of the park.
func TestParkedMessageIsScrubbedAndCapped(t *testing.T) {
	skipNonPosix(t)
	repo := initGitRepo(t)
	// A remote with a token in its userinfo, pointing nowhere.
	runGit(t, repo, "remote", "add", "origin",
		"https://user:s3cr3t-token@gh.example.invalid/x/y.git")

	r := freshRegistry(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "creds", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create should park: %v", err)
	}
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })

	q := r.Get(e.ID).Info().PendingWorktreeChoice
	if q == nil {
		t.Fatal("the unreachable remote must park the session")
	}
	if strings.Contains(q.Message, "s3cr3t-token") {
		t.Errorf("the token reached the dialog text: %q", q.Message)
	}
	if len(q.Message) > wire.MaxWorktreeChoiceMessage+4 {
		t.Errorf("message is %d bytes, over the cap", len(q.Message))
	}
}

// Retrying a PLAN-time failure must re-plan rather than replay the
// cached error — and when the re-plan lands on a different branch, the
// session name must follow it (renameEntry), or the label claims a
// branch the session is not on.
func TestRetryAfterPlanFailureReplansAndRenames(t *testing.T) {
	skipNonPosix(t)
	repo := initGitRepo(t)
	r := freshRegistry(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "planfail", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// A regular file where .worktrees must be a directory: planning
	// itself fails.
	blocker := filepath.Join(repo, ".worktrees")
	mustWriteFile(t, blocker, "not a dir\n")

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create should park: %v", err)
	}
	if r.Get(e.ID).Info().PendingWorktreeChoice == nil {
		t.Fatal("fixture: the session did not park on the plan failure")
	}
	nameWhileParked := r.Get(e.ID).Name

	// Clear the obstruction and retry: planning can now succeed.
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	if err := r.ResolveWorktreeChoice(context.Background(), e.ID,
		wire.WorktreeChoiceRetry, ""); err != nil {
		t.Fatalf("retry: %v", err)
	}
	defer r.Kill(e.ID, true)

	got := r.Get(e.ID)
	if got == nil || got.WorktreePath == "" {
		t.Fatal("the retry must re-plan and produce a worktree")
	}
	if got.WorktreeBranch == "" {
		t.Fatal("the re-plan must produce a branch")
	}
	// The name must describe the branch it actually ended up on.
	want := strings.ReplaceAll(got.WorktreeBranch, "/", "-")
	if got.Name != want {
		t.Errorf("name = %q, want %q (was %q while parked)", got.Name, want, nameWhileParked)
	}
}

// An unknown choice must not touch any state. It used to clear the
// question and then "restore" it from the same entry it had just
// nil-ed, stranding the session blocked with nothing to answer and
// only a kill to get out of.
func TestResolveRejectsUnknownChoiceWithoutTouchingState(t *testing.T) {
	r, e, _ := parkedProject(t)
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, "banana", ""); err == nil {
		t.Fatal("an unknown choice must be rejected")
	}
	info := r.Get(e.ID).Info()
	if info.PendingWorktreeChoice == nil {
		t.Fatal("the question must survive an unknown choice, or the session is unanswerable")
	}
	if info.Phase != wire.PhaseBlocked {
		t.Errorf("Phase = %q, want %q", info.Phase, wire.PhaseBlocked)
	}
	// Still answerable for real.
	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed, ""); err != nil {
		t.Fatalf("a valid choice after a rejected one must still work; got %v", err)
	}
	if r.Get(e.ID).WorktreePath == "" {
		t.Error("the session must start once answered")
	}
}

// --- create_failed: the OTHER silent fallback -------------------------
//
// These cover the `git worktree add` failure, which used to drop the
// worktree and start a plain session in the project directory without
// saying anything.

// parkedOnAddFailure parks a create at the ADD step rather than the
// fetch: the repo's origin is reachable, but the branch's worktree
// path is occupied by a file, so `git worktree add` cannot create it.
func parkedOnAddFailure(t *testing.T) (*Registry, *Entry, string) {
	t.Helper()
	skipNonPosix(t)
	repo := initGitRepo(t)
	r := freshRegistry(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "addfail", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// Make .worktrees exist but be unwritable, so branch/path
	// resolution still succeeds (the path is free) and the `git
	// worktree add` that follows fails on permissions. A regular file
	// here would instead fail path resolution, which is a different
	// code path with its own handling.
	wtDir := filepath.Join(repo, ".worktrees")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(wtDir, 0o500); err != nil {
		t.Fatal(err)
	}
	// Root ignores the mode bits, so the add would succeed and the
	// test would assert nothing. Skip rather than pass vacuously.
	if os.Geteuid() == 0 {
		t.Skip("running as root: an unwritable directory does not block the add")
	}
	t.Cleanup(func() { _ = os.Chmod(wtDir, 0o755) })
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create should park on the add failure, not fail: %v", err)
	}
	return r, e, repo
}

func TestCreateParksOnWorktreeAddFailure(t *testing.T) {
	r, e, _ := parkedOnAddFailure(t)
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })

	info := r.Get(e.ID).Info()
	if info.PendingWorktreeChoice == nil {
		t.Fatal("a failed `git worktree add` must park, not silently start a plain session")
	}
	if got := info.PendingWorktreeChoice.Kind; got != wire.WorktreeChoiceCreateFailed {
		t.Errorf("Kind = %q, want %q", got, wire.WorktreeChoiceCreateFailed)
	}
	if info.Phase != wire.PhaseBlocked {
		t.Errorf("Phase = %q, want %q", info.Phase, wire.PhaseBlocked)
	}
	if e.Alive() {
		t.Error("nothing may spawn until the user answers")
	}
}

func TestResolveAddFailureProceedStartsPlainSession(t *testing.T) {
	r, e, repo := parkedOnAddFailure(t)

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceProceed, ""); err != nil {
		t.Fatalf("resolve(proceed): %v", err)
	}
	defer r.Kill(e.ID, true)

	got := r.Get(e.ID)
	if got == nil {
		t.Fatal("proceeding must start the session")
	}
	if got.WorktreePath != "" || got.WorktreeBranch != "" {
		t.Errorf("proceeding after an add failure means NO worktree; got path=%q branch=%q",
			got.WorktreePath, got.WorktreeBranch)
	}
	// This is the old silent behaviour — now reachable only by saying so.
	if got.Info().PendingWorktreeChoice != nil {
		t.Error("the question must be cleared once answered")
	}
	_ = repo
}

func TestResolveAddFailureRetrySucceedsOnceUnblocked(t *testing.T) {
	r, e, repo := parkedOnAddFailure(t)

	// Clear the obstruction, then retry: the add now succeeds.
	if err := os.Chmod(filepath.Join(repo, ".worktrees"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceRetry, ""); err != nil {
		t.Fatalf("resolve(retry): %v", err)
	}
	defer r.Kill(e.ID, true)

	got := r.Get(e.ID)
	if got == nil || got.WorktreePath == "" {
		t.Fatal("a retry after the obstruction is cleared must produce the worktree")
	}
}

// The third silent path, found while writing the add-failure fixture:
// a REQUESTED worktree whose branch/path could not be resolved at all
// used to log and hand back a plain session in the project directory.
func TestCreateParksWhenWorktreeCannotBePlanned(t *testing.T) {
	skipNonPosix(t)
	repo := initGitRepo(t)
	r := freshRegistry(t)
	p, err := r.CreateProject(wire.CreateProjectReq{Name: "planfail", Cwd: repo})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	// A regular file where .worktrees must be a directory: every
	// candidate path under it is unusable, so resolution itself fails.
	if err := os.WriteFile(filepath.Join(repo, ".worktrees"), []byte("not a dir\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create should park, not fail: %v", err)
	}
	t.Cleanup(func() { _ = r.Kill(e.ID, true) })

	info := r.Get(e.ID).Info()
	if info.PendingWorktreeChoice == nil {
		t.Fatal("a worktree that cannot be planned must park, not silently become a plain session")
	}
	if got := info.PendingWorktreeChoice.Kind; got != wire.WorktreeChoiceCreateFailed {
		t.Errorf("Kind = %q, want %q", got, wire.WorktreeChoiceCreateFailed)
	}
	if e.Alive() {
		t.Error("nothing may spawn until the user answers")
	}
}

func TestResolveAddFailureCancelLeavesNothing(t *testing.T) {
	r, e, _ := parkedOnAddFailure(t)

	if err := r.ResolveWorktreeChoice(context.Background(), e.ID, wire.WorktreeChoiceCancel, ""); err != nil {
		t.Fatalf("resolve(cancel): %v", err)
	}
	if r.Get(e.ID) != nil {
		t.Error("cancel must leave no session behind")
	}
}
