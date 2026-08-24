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

// collectPhases drains ch until stop fires, recording the phase of
// every event for id along with its kind.
type phaseLog struct {
	mu     sync.Mutex
	events []wire.SessionEvent
}

func (p *phaseLog) add(ev wire.SessionEvent) {
	p.mu.Lock()
	p.events = append(p.events, ev)
	p.mu.Unlock()
}

// phasesFor returns "<kind>:<phase>" for every event about id, in
// arrival order.
func (p *phaseLog) phasesFor(id string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, ev := range p.events {
		if ev.Session.ID != id {
			continue
		}
		out = append(out, ev.Kind+":"+ev.Session.Phase)
	}
	return out
}

// firstAdded returns the id from the first `added` event seen, or "".
func (p *phaseLog) firstAdded() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, ev := range p.events {
		if ev.Kind == wire.SessionEventAdded {
			return ev.Session.ID
		}
	}
	return ""
}

// waitEventsFor blocks until at least n events about id have been
// recorded, then returns them.
//
// Required, not defensive: the log is filled by the watch goroutine
// draining a buffered channel, which is NOT synchronized with the
// registry call that produced the event. Asserting straight after the
// state change reads a log that can still be a beat behind — invisible
// on a fast machine, reliably fatal under a loaded CI runner (or
// GOMAXPROCS=1, which reproduces it on demand).
// waitForLastEvent polls until the most recent event for id is want.
// Prefer this over waitEventsFor when the assertion is about the state
// a session settles in: an event count is satisfied by whatever
// intermediate phases happen to have arrived, which makes the test a
// race against how many phases the path emits.
func (p *phaseLog) waitForLastEvent(t *testing.T, id, want string) []string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := p.phasesFor(id)
		if len(got) > 0 && got[len(got)-1] == want {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s on %s; got %s", want, id, joined(got))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (p *phaseLog) waitEventsFor(t *testing.T, id string, n int) []string {
	t.Helper()
	// 10s, not 3: this package now runs a lot of real git subprocesses
	// (the worktree inventory tests), so a create's event can land well
	// past three seconds on a loaded machine. The wait is a bound on
	// hanging, not a performance assertion — a passing run still
	// returns as soon as the events arrive.
	deadline := time.Now().Add(10 * time.Second)
	for {
		got := p.phasesFor(id)
		if len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			// Say so rather than returning a short slice and letting the
			// caller fail on a confusing "first event: got []".
			t.Fatalf("timed out waiting for %d events for %s; got %s", n, id, joined(got))
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// watch subscribes and records every event until the returned stop is
// called.
func watch(t *testing.T, r *Registry) (*phaseLog, func()) {
	t.Helper()
	log := &phaseLog{}
	listener, unsub := r.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for ev := range listener {
			log.add(ev)
		}
	}()
	return log, func() {
		unsub()
		<-done
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	// 10s, not 3: this package now runs a lot of real git subprocesses
	// (the worktree inventory tests), so a create's event can land well
	// past three seconds on a loaded machine. The wait is a bound on
	// hanging, not a performance assertion — a passing run still
	// returns as soon as the events arrive.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func joined(v []string) string {
	out := ""
	for i, s := range v {
		if i > 0 {
			out += " → "
		}
		out += s
	}
	return out
}

// TestCreatePhaseSequence pins the event contract the GUI's loading
// panel depends on: the entry is announced (added) in PhaseStarting
// well before the PTY exists, walks the worktree phases, and only
// reaches PhaseReady once it is actually attachable.
func TestCreatePhaseSequence(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	log, stop := watch(t, r)
	defer stop()

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID:   p.ID,
		Shell:       "/bin/bash",
		UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = r.Kill(e.ID, true) }()

	want := []string{
		"added:" + wire.PhaseStarting,
		"updated:" + wire.PhaseFetching,
		"updated:" + wire.PhaseWorktree,
		"updated:" + wire.PhaseSpawning,
		"updated:" + wire.PhaseReady,
	}
	got := log.waitEventsFor(t, e.ID, len(want))
	if len(got) != len(want) {
		t.Fatalf("phase sequence: got %s, want %s", joined(got), joined(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("phase sequence: got %s, want %s", joined(got), joined(want))
		}
	}
}

// TestCreateAddedPrecedesPTY is the freeze fix in one assertion: the
// added event must land while the session is still being spawned, so
// the GUI has a tile to render a loading panel into.
func TestCreateAddedPrecedesPTY(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)

	release := make(chan struct{})
	restore := SetStartSessionForTest(func(opts session.Options) (*session.Session, error) {
		<-release
		return session.Start(opts)
	})
	t.Cleanup(restore)

	listener, unsub := r.Subscribe()
	defer unsub()

	go func() {
		_, _ = r.Create(context.Background(), wire.CreateSpec{Name: "slow", Shell: "/bin/bash"})
	}()

	var added wire.SessionEvent
	select {
	case ev := <-listener:
		added = ev
	case <-time.After(2 * time.Second):
		t.Fatal("no added event while the spawn was blocked")
	}
	if added.Kind != wire.SessionEventAdded {
		t.Fatalf("first event: got %s, want added", added.Kind)
	}
	if added.Session.Alive {
		t.Error("added event claims alive:true before the PTY exists")
	}
	if added.Session.Phase != wire.PhaseStarting {
		t.Errorf("added phase: got %q, want %q", added.Session.Phase, wire.PhaseStarting)
	}
	close(release)
	waitFor(t, "ready", func() bool { return r.Phase(added.Session.ID) == wire.PhaseReady })
	_ = r.Kill(added.Session.ID, true)
}

// TestCreateBornDeadEndsReady covers the agent-binary-missing case.
// The GUI's dead overlay keys off alive:false at PhaseReady, so a
// failed spawn must not strand the entry in a pending phase — that
// would leave a tile spinning forever.
func TestCreateBornDeadEndsReady(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)

	restore := SetStartSessionForTest(func(session.Options) (*session.Session, error) {
		return nil, os.ErrNotExist
	})
	t.Cleanup(restore)

	log, stop := watch(t, r)
	defer stop()

	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "doomed", Shell: "/bin/bash"})
	if err == nil {
		t.Fatal("Create: expected the spawn failure to surface")
	}
	if e == nil {
		t.Fatal("Create: entry should be stranded, not dropped")
	}
	// added(starting) … updated(ready) carrying the failure. Wait for
	// the TERMINAL event, not for a count: the create also emits
	// updated(spawning) in between, so "two events" can be satisfied
	// before ready arrives — which showed up as a flake once this
	// package got slower.
	got := log.waitForLastEvent(t, e.ID, "updated:"+wire.PhaseReady)
	if len(got) == 0 || got[0] != "added:"+wire.PhaseStarting {
		t.Fatalf("first event: got %s", joined(got))
	}
	last := got[len(got)-1]
	if last != "updated:"+wire.PhaseReady {
		t.Errorf("last event: got %q, want updated:ready — a born-dead session must not stay pending", last)
	}
	info := r.Get(e.ID).Info()
	if info.Alive || info.LastError == "" {
		t.Errorf("born-dead entry: %+v", info)
	}
}

// TestKillPhaseSequence pins the teardown contract: the client is told
// we're checking the worktree, then closing, and only then is the
// session removed.
func TestKillPhaseSequence(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	log, stop := watch(t, r)
	defer stop()

	if err := r.Kill(e.ID, false); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	want := []string{
		"updated:" + wire.PhaseChecking,
		"updated:" + wire.PhaseClosing,
	}
	// ...plus the `removed` that must follow them.
	got := log.waitEventsFor(t, e.ID, len(want)+1)
	for i, w := range want {
		if i >= len(got) || got[i] != w {
			t.Fatalf("kill sequence: got %s, want %s → removed", joined(got), joined(want))
		}
	}
	if last := got[len(got)-1]; last[:len("removed")] != "removed" {
		t.Errorf("kill sequence must end in removed, got %s", joined(got))
	}
}

// TestKillDirtyRefusalRestoresReady: a cancelled confirm dialog must
// not leave the tile stuck on "Closing…".
func TestKillDirtyRefusalRestoresReady(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	e, err := r.Create(context.Background(), wire.CreateSpec{
		ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	defer func() { _ = r.Kill(e.ID, true) }()
	if e.WorktreePath == "" {
		t.Skip("no worktree materialized")
	}
	if err := os.WriteFile(filepath.Join(e.WorktreePath, "dirt.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("dirty: %v", err)
	}

	log, stop := watch(t, r)
	defer stop()

	if err := r.Kill(e.ID, false); err != ErrWorktreeDirty {
		t.Fatalf("Kill: got %v, want ErrWorktreeDirty", err)
	}
	if got := r.Phase(e.ID); got != wire.PhaseReady {
		t.Errorf("phase after refusal: got %q, want ready", got)
	}
	want := []string{"updated:" + wire.PhaseChecking, "updated:" + wire.PhaseReady}
	got := log.waitEventsFor(t, e.ID, len(want))
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("refusal sequence: got %s, want %s", joined(got), joined(want))
	}
	if r.Get(e.ID) == nil {
		t.Error("refused kill removed the entry")
	}
}

// TestKillDuringCreate: killing a session while its worktree/PTY are
// still coming up must leave neither a ghost entry nor a live process.
func TestKillDuringCreate(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)

	release := make(chan struct{})
	spawned := make(chan *session.Session, 1)
	restore := SetStartSessionForTest(func(opts session.Options) (*session.Session, error) {
		<-release
		s, err := session.Start(opts)
		if err == nil {
			spawned <- s
		}
		return s, err
	})
	// Release on EVERY exit path: a t.Fatalf before the close below
	// would otherwise strand the create goroutine on <-release forever.
	var released bool
	releaseOnce := func() {
		if !released {
			released = true
			close(release)
		}
	}
	t.Cleanup(func() {
		releaseOnce()
		restore()
	})

	listener, unsub := r.Subscribe()
	defer unsub()

	createDone := make(chan error, 1)
	go func() {
		_, err := r.Create(context.Background(), wire.CreateSpec{Name: "doomed", Shell: "/bin/bash"})
		createDone <- err
	}()

	var id string
	select {
	case ev := <-listener:
		id = ev.Session.ID
	case <-time.After(2 * time.Second):
		t.Fatal("no added event")
	}

	// Kill lands while the spawn is still blocked: the entry goes, and
	// the create tail is left holding a PTY nobody owns.
	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	releaseOnce()

	if err := <-createDone; err != ErrNotFound {
		t.Errorf("Create after mid-create kill: got %v, want ErrNotFound", err)
	}
	if r.Get(id) != nil {
		t.Error("ghost entry survived the mid-create kill")
	}
	select {
	case s := <-spawned:
		select {
		case <-s.Done():
		case <-time.After(3 * time.Second):
			t.Error("PTY spawned mid-create was leaked (still running)")
		}
	case <-time.After(time.Second):
		// Spawn never happened; nothing to leak.
	}
}

// TestConcurrentCreatesInOneRepo proves gitMu: two `git worktree add`
// runs against the same repo would otherwise race on index.lock.
func TestConcurrentCreatesInOneRepo(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	var wg sync.WaitGroup
	entries := make([]*Entry, 2)
	errs := make([]error, 2)
	for i := range entries {
		wg.Add(1)
		go func() {
			defer wg.Done()
			entries[i], errs[i] = r.Create(context.Background(), wire.CreateSpec{
				ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
			})
		}()
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent create %d: %v", i, err)
		}
		if entries[i].WorktreePath == "" {
			t.Errorf("concurrent create %d got no worktree (git lock contention?)", i)
		}
		defer func() { _ = r.Kill(entries[i].ID, true) }()
	}
	if entries[0].WorktreePath == entries[1].WorktreePath {
		t.Errorf("both creates landed in the same worktree %q", entries[0].WorktreePath)
	}
}

// TestKillDuringCreate_SpawnFails covers the other half of the
// mid-create kill: the spawn doesn't just get orphaned, it fails.
//
// Two things must not happen. The entry is already gone, so a
// broadcast here would emit an `updated` after the `removed` clients
// have seen — which a liveness-tracking client reads as a session
// dying (the GUI fires a "Session ended" notification for a session
// the user just closed). And the worktree created moments earlier is
// nobody else's to clean up: Kill ran before attachSession recorded
// the path on the entry, so it saw no worktree at all.
func TestKillDuringCreate_SpawnFails(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	atSpawn := make(chan string, 1)
	release := make(chan struct{})
	restore := SetStartSessionForTest(func(session.Options) (*session.Session, error) {
		atSpawn <- "" // the worktree exists by now; the PTY does not
		<-release
		return nil, os.ErrNotExist
	})
	var released bool
	releaseOnce := func() {
		if !released {
			released = true
			close(release)
		}
	}
	t.Cleanup(func() {
		releaseOnce()
		restore()
	})

	log, stop := watch(t, r)
	defer stop()

	createDone := make(chan error, 1)
	go func() {
		_, err := r.Create(context.Background(), wire.CreateSpec{
			ProjectID: p.ID, Shell: "/bin/bash", UseWorktree: true,
		})
		createDone <- err
	}()

	// Wait until the create is parked in the spawn: the worktree is on
	// disk and the entry is registered, which is the exact window a
	// user's ⌘W lands in.
	select {
	case <-atSpawn:
	case <-time.After(5 * time.Second):
		t.Fatal("create never reached the spawn")
	}
	id := ""
	waitFor(t, "the added event", func() bool {
		id = log.firstAdded()
		return id != ""
	})
	wtDirs := worktreeDirs(t, p.Cwd)
	if len(wtDirs) != 1 {
		t.Fatalf("expected exactly one worktree before the kill, got %v", wtDirs)
	}

	if err := r.Kill(id, true); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	releaseOnce()

	if err := <-createDone; err != ErrNotFound {
		t.Errorf("Create after mid-create kill: got %v, want ErrNotFound", err)
	}
	// The last word about this session must be `removed`: added,
	// the create phases, the kill phases, then removed.
	got := log.waitEventsFor(t, id, 2)
	waitFor(t, "the removed event", func() bool {
		ev := log.phasesFor(id)
		return len(ev) > 0 && strings.HasPrefix(ev[len(ev)-1], "removed")
	})
	// Settle: give a stray post-removal broadcast time to land, so
	// this asserts nothing follows rather than merely getting there first.
	time.Sleep(50 * time.Millisecond)
	got = log.phasesFor(id)
	if len(got) == 0 {
		t.Fatalf("no events for %s", id)
	}
	if last := got[len(got)-1]; !strings.HasPrefix(last, "removed") {
		t.Errorf("events after the kill: %s — nothing may follow `removed`", joined(got))
	}
	// ...and the worktree it made must not outlive it.
	waitFor(t, "the orphaned worktree to be discarded", func() bool {
		return len(worktreeDirs(t, p.Cwd)) == 0
	})
}

// worktreeDirs lists the entries under <repo>/.worktrees, or nothing
// when the directory doesn't exist yet.
func worktreeDirs(t *testing.T, repo string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repo, ".worktrees"))
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// TestReviveWithPhaseSequence pins the contract the daemon's
// background boot revive depends on: a session persisted from a
// previous run announces PhaseSpawning *before* its PTY is forked and
// only clears to PhaseReady once it is attachable. Without the
// bracket the entry sits at alive:false + PhaseReady, which
// serveAttach reports as "session_dead" — the wrong answer for a
// session that is merely still coming up.
func TestReviveWithPhaseSequence(t *testing.T) {
	skipNonPosix(t)
	dir := t.TempDir()

	r1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	e, err := r1.Create(context.Background(), wire.CreateSpec{Name: "persisted", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := e.ID
	// Close kills the PTY but keeps the metadata: exactly the state a
	// daemon restart loads from disk.
	_ = r1.Close()

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	if info := infoFor(t, r2, id); info.Alive {
		t.Fatalf("reloaded session %s is alive before revive", id)
	}

	log, stop := watch(t, r2)
	defer stop()

	revived, err := r2.ReviveWithPhase(id, session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24})
	if err != nil {
		t.Fatalf("ReviveWithPhase: %v", err)
	}
	if !revived {
		t.Fatal("ReviveWithPhase declined an idle session")
	}

	got := log.waitForLastEvent(t, id, "updated:"+wire.PhaseReady)
	if got[0] != "updated:"+wire.PhaseSpawning {
		t.Fatalf("phase sequence: got %s, want spawning first", joined(got))
	}
	if info := infoFor(t, r2, id); !info.Alive {
		t.Fatalf("session %s not alive after ReviveWithPhase", id)
	}
}

// infoFor returns the SessionInfo for id, failing if it is gone.
func infoFor(t *testing.T, r *Registry, id string) wire.SessionInfo {
	t.Helper()
	for _, info := range r.List() {
		if info.ID == id {
			return info
		}
	}
	t.Fatalf("session %s not in registry", id)
	return wire.SessionInfo{}
}

// TestReviveWithPhaseDoesNotClobberAKill pins the compare-and-set on
// the way out of the phase bracket. Revive spawns outside r.mu, and a
// client kill landing in that window has already moved the entry to
// PhaseChecking/PhaseClosing — clearing unconditionally to ready
// would drop the GUI's spinner and show a ready tile for a session
// being torn down.
func TestReviveWithPhaseDoesNotClobberAKill(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{Name: "doomed", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	waitFor(t, "session ready", func() bool { return r.Phase(e.ID) == wire.PhaseReady })

	// Stand in for the kill that arrives mid-spawn: the entry is no
	// longer idle, so the revive must decline it outright.
	if !r.setPhaseIf(e.ID, wire.PhaseReady, wire.PhaseClosing) {
		t.Fatal("could not move the entry to closing")
	}
	revived, err := r.ReviveWithPhase(e.ID, session.Options{Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("ReviveWithPhase: %v", err)
	}
	if revived {
		t.Fatal("ReviveWithPhase claimed an entry a kill already owned")
	}
	if got := r.Phase(e.ID); got != wire.PhaseClosing {
		t.Fatalf("phase = %q after a declined revive, want %q", got, wire.PhaseClosing)
	}
	_ = r.Kill(e.ID, true)
}

// TestReviveDoesNotResurrectAKilledEntry covers the other half: the
// spawn itself lands after the kill removed the entry. Binding the
// fresh PTY to that orphaned Entry leaks the process — watchSessionExit
// finds no entry and returns, so nobody ever closes it, and its cwd
// sits inside a worktree the kill already removed.
func TestReviveDoesNotResurrectAKilledEntry(t *testing.T) {
	skipNonPosix(t)
	dir := t.TempDir()
	r1, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	e, err := r1.Create(context.Background(), wire.CreateSpec{Name: "racy", Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	id := e.ID
	_ = r1.Close()

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })

	// Delete the entry while the revive is in flight, the way kill
	// does. Racing a goroutine against a real spawn is what makes
	// this reproduce; the assertion holds either way it lands.
	done := make(chan error, 1)
	go func() { done <- r2.Revive(id, session.Options{Shell: "/bin/bash", Cols: 80, Rows: 24}) }()
	_ = r2.Kill(id, true)

	if err := <-done; err != nil && !errors.Is(err, ErrNotFound) {
		t.Fatalf("Revive: %v", err)
	}
	// Whoever won, no live PTY may be reachable through a deleted
	// entry — and if the revive won the race the entry is simply gone.
	if got := r2.Get(id); got != nil && got.Session() != nil {
		t.Fatal("a killed entry came back with a live PTY attached")
	}
	for _, info := range r2.List() {
		if info.ID == id {
			t.Fatal("killed session is still listed")
		}
	}
}
