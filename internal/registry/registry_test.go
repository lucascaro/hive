package registry

import (
	"runtime"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

func skipOnWindows(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("registry tests start sessions; require POSIX shell")
	}
}

func freshRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = r.Close() })
	return r
}

func TestCreateListKill(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)

	listener, unsub := r.Subscribe()
	defer unsub()

	a, err := r.Create(wire.CreateSpec{Name: "alpha", Color: "#abc", Cols: 80, Rows: 24, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create alpha: %v", err)
	}
	b, err := r.Create(wire.CreateSpec{Name: "beta", Color: "#def", Cols: 80, Rows: 24, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create beta: %v", err)
	}

	expectEvent := func(kind string) wire.SessionEvent {
		select {
		case ev := <-listener:
			if ev.Kind != kind {
				t.Errorf("event kind: got %s, want %s", ev.Kind, kind)
			}
			return ev
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s event", kind)
			return wire.SessionEvent{}
		}
	}
	expectEvent("added") // alpha
	expectEvent("added") // beta

	got := r.List()
	if len(got) != 2 {
		t.Fatalf("List: got %d entries, want 2", len(got))
	}
	if got[0].Name != "alpha" || got[1].Name != "beta" {
		t.Errorf("order: %+v", got)
	}
	if got[0].Order != 0 || got[1].Order != 1 {
		t.Errorf("order numbers: %+v", got)
	}

	if err := r.Kill(a.ID); err != nil {
		t.Fatalf("Kill alpha: %v", err)
	}
	expectEvent("removed")

	got = r.List()
	if len(got) != 1 || got[0].ID != b.ID || got[0].Order != 0 {
		t.Errorf("after kill: %+v", got)
	}
}

func TestUpdateRenameAndColor(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	a, _ := r.Create(wire.CreateSpec{Name: "x", Shell: "/bin/bash"})

	newName := "renamed"
	newColor := "#fedcba"
	_, err := r.Update(wire.UpdateSessionReq{
		SessionID: a.ID,
		Name:      &newName,
		Color:     &newColor,
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}

	got := r.List()
	if got[0].Name != newName || got[0].Color != newColor {
		t.Errorf("after update: %+v", got[0])
	}
}

func TestReorder(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	a, _ := r.Create(wire.CreateSpec{Name: "a", Shell: "/bin/bash"})
	b, _ := r.Create(wire.CreateSpec{Name: "b", Shell: "/bin/bash"})
	c, _ := r.Create(wire.CreateSpec{Name: "c", Shell: "/bin/bash"})

	// Move c to position 0.
	zero := 0
	if _, err := r.Update(wire.UpdateSessionReq{SessionID: c.ID, Order: &zero}); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	got := r.List()
	want := []string{c.ID, a.ID, b.ID}
	for i, w := range want {
		if got[i].ID != w {
			t.Errorf("pos %d: got %s, want %s", i, got[i].ID, w)
		}
		if got[i].Order != i {
			t.Errorf("pos %d order: got %d, want %d", i, got[i].Order, i)
		}
	}
}

func TestPersistenceAcrossOpen(t *testing.T) {
	skipOnWindows(t)
	dir := t.TempDir()

	r1, _ := Open(dir)
	a, _ := r1.Create(wire.CreateSpec{Name: "first", Color: "#111", Shell: "/bin/bash"})
	b, _ := r1.Create(wire.CreateSpec{Name: "second", Color: "#222", Shell: "/bin/bash"})
	_ = a
	_ = b
	_ = r1.Close()

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer r2.Close()

	got := r2.List()
	if len(got) != 2 {
		t.Fatalf("after reopen: %d entries", len(got))
	}
	if got[0].Name != "first" || got[0].Color != "#111" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Name != "second" || got[1].Color != "#222" {
		t.Errorf("second: %+v", got[1])
	}
	// Sessions are not auto-restarted: alive should be false.
	if got[0].Alive || got[1].Alive {
		t.Errorf("expected entries to be inactive after reopen")
	}
}
