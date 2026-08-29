package registry

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/wire"
)

// orderIDs returns the registry's global order slice, plus a check that
// every entry's Order field matches its index in it (reindexLocked's
// invariant, which every r.order mutation must preserve).
func orderIDs(t *testing.T, r *Registry) []string {
	t.Helper()
	r.mu.Lock()
	defer r.mu.Unlock()
	for i, id := range r.order {
		e := r.entries[id]
		if e == nil {
			t.Fatalf("order[%d]=%s has no entry", i, id)
		}
		if e.Order != i {
			t.Errorf("entry %s: Order=%d, want %d (index in r.order)", id, e.Order, i)
		}
	}
	return slices.Clone(r.order)
}

func mustCreate(t *testing.T, r *Registry, spec wire.CreateSpec) *Entry {
	t.Helper()
	spec.Cols, spec.Rows, spec.Shell = 80, 24, "/bin/bash"
	e, err := r.Create(context.Background(), spec)
	if err != nil {
		t.Fatalf("Create %q: %v", spec.Name, err)
	}
	return e
}

// drain empties a listener channel of events already queued, so a test
// can assert on only the events a later action produces.
func drain(ch Listener) {
	for {
		select {
		case <-ch:
		default:
			return
		}
	}
}

func TestCreateInsertsAfterAnchor(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	s1 := mustCreate(t, r, wire.CreateSpec{Name: "s1"})
	s2 := mustCreate(t, r, wire.CreateSpec{Name: "s2"})
	s3 := mustCreate(t, r, wire.CreateSpec{Name: "s3"})

	n := mustCreate(t, r, wire.CreateSpec{Name: "new", InsertAfterSessionID: s2.ID})

	want := []string{s1.ID, s2.ID, n.ID, s3.ID}
	if got := orderIDs(t, r); !slices.Equal(got, want) {
		t.Errorf("order: got %v, want %v", got, want)
	}
}

func TestCreateInsertAfterUnknownAnchorAppends(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	s1 := mustCreate(t, r, wire.CreateSpec{Name: "s1"})
	s2 := mustCreate(t, r, wire.CreateSpec{Name: "s2"})

	n := mustCreate(t, r, wire.CreateSpec{Name: "new", InsertAfterSessionID: uuid.NewString()})

	want := []string{s1.ID, s2.ID, n.ID}
	if got := orderIDs(t, r); !slices.Equal(got, want) {
		t.Errorf("order: got %v, want %v", got, want)
	}
}

func TestCreateInsertAfterCrossProjectAnchorAppends(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	pa, err := r.CreateProject(wire.CreateProjectReq{Name: "A"})
	if err != nil {
		t.Fatalf("CreateProject A: %v", err)
	}
	pb, err := r.CreateProject(wire.CreateProjectReq{Name: "B"})
	if err != nil {
		t.Fatalf("CreateProject B: %v", err)
	}

	a1 := mustCreate(t, r, wire.CreateSpec{Name: "a1", ProjectID: pa.ID})
	b1 := mustCreate(t, r, wire.CreateSpec{Name: "b1", ProjectID: pb.ID})

	// Anchor lives in project A, new session belongs to project B: the
	// splice would drop it inside A's index range, so it must append.
	n := mustCreate(t, r, wire.CreateSpec{Name: "b2", ProjectID: pb.ID, InsertAfterSessionID: a1.ID})

	want := []string{a1.ID, b1.ID, n.ID}
	if got := orderIDs(t, r); !slices.Equal(got, want) {
		t.Errorf("order: got %v, want %v", got, want)
	}
}

func TestCreateInsertAfterBroadcastsShiftedOrders(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	s1 := mustCreate(t, r, wire.CreateSpec{Name: "s1"})
	s2 := mustCreate(t, r, wire.CreateSpec{Name: "s2"})
	s3 := mustCreate(t, r, wire.CreateSpec{Name: "s3"})

	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	n := mustCreate(t, r, wire.CreateSpec{Name: "new", InsertAfterSessionID: s1.ID})

	updated := map[string]int{}
	added := 0
	deadline := time.After(2 * time.Second)
collect:
	for {
		select {
		case ev := <-listener:
			switch ev.Kind {
			case wire.SessionEventUpdated:
				// The new session emits its own updated events as it
				// walks the create phases; this test is about the
				// order shift on its *siblings*.
				if ev.Session.ID == n.ID {
					continue
				}
				updated[ev.Session.ID] = ev.Session.Order
			case wire.SessionEventAdded:
				added++
				if ev.Session.ID != n.ID {
					t.Errorf("added event for %s, want %s", ev.Session.ID, n.ID)
				}
			}
			if added > 0 && len(updated) >= 2 {
				break collect
			}
		case <-deadline:
			break collect
		}
	}

	if added != 1 {
		t.Errorf("added events: got %d, want 1", added)
	}
	// s2 and s3 shifted down by one; s1 kept index 0.
	if got, ok := updated[s2.ID]; !ok || got != 2 {
		t.Errorf("updated order for s2: got %d (present=%v), want 2", got, ok)
	}
	if got, ok := updated[s3.ID]; !ok || got != 3 {
		t.Errorf("updated order for s3: got %d (present=%v), want 3", got, ok)
	}
}

func TestCreateAppendEmitsNoShiftedUpdates(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	mustCreate(t, r, wire.CreateSpec{Name: "s1"})
	mustCreate(t, r, wire.CreateSpec{Name: "s2"})

	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	n := mustCreate(t, r, wire.CreateSpec{Name: "new"})

	// Give any (unwanted) fan-out time to land before asserting.
	time.Sleep(100 * time.Millisecond)
	for {
		select {
		case ev := <-listener:
			if ev.Kind == wire.SessionEventTitle {
				// The program on the PTY re-titled itself (a shell does
				// this from its prompt). Asynchronous and unrelated to
				// ordering — which is precisely why titles have their own
				// kind rather than riding `updated`.
				continue
			}
			if ev.Session.ID != n.ID {
				t.Errorf("unexpected %s event for %s on a plain append", ev.Kind, ev.Session.ID)
				continue
			}
			// The new session's own added + phase updates are
			// expected; nobody else may be touched by a plain append.
		default:
			return
		}
	}
}

func TestInsertEntryRollbackRestoresOrder(t *testing.T) {
	skipOnWindows(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory mode bits, so the persist would succeed")
	}
	r := freshRegistry(t)

	s1 := mustCreate(t, r, wire.CreateSpec{Name: "s1"})
	mustCreate(t, r, wire.CreateSpec{Name: "s2"})
	before := orderIDs(t, r)

	// Make the sessions dir unwritable so persistEntryLocked fails.
	dir := SessionsDir(r.stateDir)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	id := uuid.NewString()
	if _, err := r.insertEntry(
		wire.CreateSpec{InsertAfterSessionID: s1.ID},
		createPlan{id: id, projectID: s1.ProjectID, name: "doomed"},
	); err == nil {
		t.Fatal("insertEntry: got nil error, want a persist failure")
	}
	if _, err := os.Stat(filepath.Join(dir, id)); err == nil {
		t.Error("doomed session dir was created despite the failure")
	}

	if got := orderIDs(t, r); !slices.Equal(got, before) {
		t.Errorf("order after rollback: got %v, want %v", got, before)
	}
	r.mu.Lock()
	_, present := r.entries[id]
	r.mu.Unlock()
	if present {
		t.Error("rolled-back entry is still in r.entries")
	}
}

func TestUpdateOrderMoveDownAndUpAcrossGlobalList(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	s := make([]*Entry, 4)
	for i := range s {
		s[i] = mustCreate(t, r, wire.CreateSpec{Name: string(rune('a' + i))})
	}

	move := func(e *Entry, to int) {
		t.Helper()
		if _, err := r.Update(wire.UpdateSessionReq{SessionID: e.ID, Order: &to}); err != nil {
			t.Fatalf("Update(%s, %d): %v", e.Name, to, err)
		}
	}

	// Down: moveInOrder deletes first, so the target index is the
	// pre-move index of the session being jumped over.
	move(s[1], 2)
	want := []string{s[0].ID, s[2].ID, s[1].ID, s[3].ID}
	if got := orderIDs(t, r); !slices.Equal(got, want) {
		t.Fatalf("after move down: got %v, want %v", got, want)
	}

	// Up: same call shape, target index below the current one.
	move(s[1], 1)
	want = []string{s[0].ID, s[1].ID, s[2].ID, s[3].ID}
	if got := orderIDs(t, r); !slices.Equal(got, want) {
		t.Fatalf("after move up: got %v, want %v", got, want)
	}

	// Wrap to the end: an index at (or past) the tail clamps to last.
	move(s[0], 3)
	want = []string{s[1].ID, s[2].ID, s[3].ID, s[0].ID}
	if got := orderIDs(t, r); !slices.Equal(got, want) {
		t.Fatalf("after move to tail: got %v, want %v", got, want)
	}
}

// --- Kill compaction -------------------------------------------------
//
// Order is the index into r.order, and both GUI reorder paths hand a
// sibling's .order straight back as an absolute index. A kill that left
// holes behind used to break that: the holes made a later append reuse
// an Order another entry already held, and every subsequent move
// clamped or landed in the wrong slot.

func TestKillCompactsOrderAndKeepsAppendsUnique(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	a := mustCreate(t, r, wire.CreateSpec{Name: "a"})
	b := mustCreate(t, r, wire.CreateSpec{Name: "b"})
	c := mustCreate(t, r, wire.CreateSpec{Name: "c"})

	if err := r.Kill(a.ID, true); err != nil {
		t.Fatalf("Kill a: %v", err)
	}
	// orderIDs asserts Order == index for every entry.
	if got := orderIDs(t, r); !slices.Equal(got, []string{b.ID, c.ID}) {
		t.Fatalf("after kill: got %v, want [b c]", got)
	}

	d := mustCreate(t, r, wire.CreateSpec{Name: "d"})
	if got := orderIDs(t, r); !slices.Equal(got, []string{b.ID, c.ID, d.ID}) {
		t.Fatalf("after append: got %v, want [b c d]", got)
	}
	seen := map[int]string{}
	for _, info := range r.List() {
		if prev, dup := seen[info.Order]; dup {
			t.Fatalf("duplicate Order %d on %s and %s", info.Order, prev, info.ID)
		}
		seen[info.Order] = info.ID
	}
	for i := range 3 {
		if _, ok := seen[i]; !ok {
			t.Fatalf("Order %d missing — sequence is sparse: %v", i, seen)
		}
	}
}

func TestKillBroadcastsShiftedSessions(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	a := mustCreate(t, r, wire.CreateSpec{Name: "a"})
	b := mustCreate(t, r, wire.CreateSpec{Name: "b"})
	c := mustCreate(t, r, wire.CreateSpec{Name: "c"})

	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	if err := r.Kill(a.ID, true); err != nil {
		t.Fatalf("Kill a: %v", err)
	}

	// Collect until both survivors report their new Order, or time out.
	want := map[string]int{b.ID: 0, c.ID: 1}
	got := map[string]int{}
	deadline := time.After(2 * time.Second)
	for len(got) < len(want) {
		select {
		case ev := <-listener:
			if ev.Kind != wire.SessionEventUpdated {
				continue
			}
			if _, ok := want[ev.Session.ID]; ok {
				got[ev.Session.ID] = ev.Session.Order
			}
		case <-deadline:
			t.Fatalf("timed out; got %v, want %v", got, want)
		}
	}
	for id, order := range want {
		if got[id] != order {
			t.Errorf("session %s: broadcast Order=%d, want %d", id, got[id], order)
		}
	}
}

func TestMoveAfterKillLandsWhereAsked(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	s := make([]*Entry, 4)
	for i := range s {
		s[i] = mustCreate(t, r, wire.CreateSpec{Name: string(rune('a' + i))})
	}
	if err := r.Kill(s[1].ID, true); err != nil {
		t.Fatalf("Kill s1: %v", err)
	}

	// What the GUI does: read the target sibling's .order off List()
	// and hand it back as the absolute index to splice at.
	var target int
	for _, info := range r.List() {
		if info.ID == s[0].ID {
			target = info.Order
		}
	}
	if _, err := r.Update(wire.UpdateSessionReq{SessionID: s[3].ID, Order: &target}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	want := []string{s[3].ID, s[0].ID, s[2].ID}
	if got := orderIDs(t, r); !slices.Equal(got, want) {
		t.Fatalf("after move: got %v, want %v", got, want)
	}
}

func TestLoadDerivesOrderFromIndexNotMeta(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	ids := make([]string, 3)
	for i := range ids {
		ids[i] = mustCreate(t, r, wire.CreateSpec{Name: string(rune('a' + i))}).ID
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Skew every session.json the way a pre-compaction build would
	// have left it: sparse, out of step with index.json's slice.
	for i, id := range ids {
		path := filepath.Join(SessionsDir(dir), id, "session.json")
		var meta MetaFile
		if err := readJSON(path, &meta); err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		meta.Order = 90 - i*10
		if err := writeJSON(path, meta); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	// orderIDs asserts Order == index; the skewed (and reversed) meta
	// values must lose to index.json.
	if got := orderIDs(t, r2); !slices.Equal(got, ids) {
		t.Fatalf("after reload: got %v, want %v", got, ids)
	}
}

func TestKillProjectCompactsProjectOrder(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	made := make([]*Project, 3)
	for i := range made {
		p, err := r.CreateProject(wire.CreateProjectReq{Name: string(rune('a' + i))})
		if err != nil {
			t.Fatalf("CreateProject: %v", err)
		}
		made[i] = p
	}
	if err := r.KillProject(made[0].ID, false); err != nil {
		t.Fatalf("KillProject: %v", err)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	for i, id := range r.projectOrder {
		p := r.projects[id]
		if p == nil {
			t.Fatalf("projectOrder[%d]=%s has no project", i, id)
		}
		if p.Order != i {
			t.Errorf("project %s: Order=%d, want %d", p.Name, p.Order, i)
		}
	}
}

// TestKillBroadcastsSurvivorsReadAfterTeardown pins the ordering of
// Kill's fan-out against its own teardown. Kill releases r.mu, then
// closes the PTY and runs `git worktree remove` — seconds on a real
// worktree — before it broadcasts. Reading the survivors BEFORE that
// window and sending the snapshot after it clobbers whatever ran
// meanwhile, which is how a stale .order comes back: the late
// broadcast overwrites the client's correct values with pre-teardown
// ones, and the next reorder (which sends an absolute index) misfires.
//
// r.gitMu is the seam: force=true skips the dirty pre-flight, so the
// kill's only git work is the cleanup, and holding gitMu parks it
// exactly inside the window with r.mu already released. The mid-window
// mutation is a reorder rather than a create because the create path
// takes gitMu too (create.go:263, worktree adoption) and would
// deadlock against the very lock this test is holding.
func TestKillBroadcastsSurvivorsReadAfterTeardown(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)

	// w owns the worktree, so killing it takes the git-cleanup path.
	w := mustCreate(t, r, wire.CreateSpec{
		Name: "w", ProjectID: p.ID, UseWorktree: true,
	})
	a := mustCreate(t, r, wire.CreateSpec{Name: "a", ProjectID: p.ID})
	b := mustCreate(t, r, wire.CreateSpec{Name: "b", ProjectID: p.ID})

	listener, unsub := r.Subscribe()
	defer unsub()

	r.gitMu.Lock() // freeze the teardown before it starts
	killed := make(chan error, 1)
	go func() { killed <- r.Kill(w.ID, true) }()

	// Wait until the kill has passed the locked section (w is gone from
	// the registry) and is parked on gitMu.
	deadline := time.After(5 * time.Second)
	for len(r.List()) > 2 {
		select {
		case <-deadline:
			r.gitMu.Unlock()
			t.Fatal("kill never reached the teardown window")
		default:
			time.Sleep(5 * time.Millisecond)
		}
	}

	// Mid-window: move b above a. A snapshot taken before this now
	// describes the opposite order.
	drain(listener)
	top := 0
	if _, err := r.Update(wire.UpdateSessionReq{SessionID: b.ID, Order: &top}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	r.gitMu.Unlock()
	if err := <-killed; err != nil {
		t.Fatalf("Kill w: %v", err)
	}

	if got := orderIDs(t, r); !slices.Equal(got, []string{b.ID, a.ID}) {
		t.Fatalf("registry order: got %v, want [b a]", got)
	}
	// Whatever the fan-out said LAST for each session must agree with
	// where that session actually sits. Kill broadcasts before it
	// returns, so everything is queued by now — read until the stream
	// goes quiet rather than stopping at the first sighting of each
	// id, which would only ever see the reorder's (correct) events and
	// never the kill's.
	want := map[string]int{b.ID: 0, a.ID: 1}
	last := map[string]int{}
	for draining := true; draining; {
		select {
		case ev := <-listener:
			if ev.Kind != wire.SessionEventUpdated {
				continue
			}
			if _, ok := want[ev.Session.ID]; ok {
				last[ev.Session.ID] = ev.Session.Order
			}
		case <-time.After(500 * time.Millisecond):
			draining = false
		}
	}
	if len(last) != len(want) {
		t.Fatalf("saw %v, want an update for each of %v", last, want)
	}
	for id, order := range want {
		if last[id] != order {
			t.Errorf("session %s: last broadcast Order=%d, want %d", id, last[id], order)
		}
	}
}
