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
// every entry's Order field matches its index in it (renumberLocked's
// invariant, which the splice must preserve).
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
