package registry

import (
	"context"
	"os"
	"path/filepath"
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
	deadline := time.Now().Add(3 * time.Second)
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

	waitFor(t, "ready phase", func() bool { return r.Phase(e.ID) == wire.PhaseReady })

	got := log.phasesFor(e.ID)
	want := []string{
		"added:" + wire.PhaseStarting,
		"updated:" + wire.PhaseFetching,
		"updated:" + wire.PhaseWorktree,
		"updated:" + wire.PhaseSpawning,
		"updated:" + wire.PhaseReady,
	}
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
	waitFor(t, "ready phase", func() bool { return r.Phase(e.ID) == wire.PhaseReady })

	got := log.phasesFor(e.ID)
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
	got := log.phasesFor(e.ID)
	want := []string{
		"updated:" + wire.PhaseChecking,
		"updated:" + wire.PhaseClosing,
	}
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
	got := log.phasesFor(e.ID)
	want := []string{"updated:" + wire.PhaseChecking, "updated:" + wire.PhaseReady}
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
	t.Cleanup(restore)

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
	close(release)

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
