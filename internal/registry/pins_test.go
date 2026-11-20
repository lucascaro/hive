package registry

import (
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

func mustPin(t *testing.T, r *Registry, id, title string) wire.Pin {
	t.Helper()
	p, err := r.AddPin(id, title, "body of "+title)
	if err != nil {
		t.Fatalf("AddPin(%q): %v", title, err)
	}
	return p
}

func pinTitles(t *testing.T, r *Registry, id string) []string {
	t.Helper()
	pins, err := r.Pins(id)
	if err != nil {
		t.Fatalf("Pins: %v", err)
	}
	var out []string
	for _, p := range pins {
		out = append(out, p.Title)
	}
	return out
}

func TestAddPinPersistsAndOrdersNewestFirst(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	for _, title := range []string{"a", "b", "c"} {
		mustPin(t, r, e.ID, title)
	}
	if got := strings.Join(pinTitles(t, r, e.ID), ","); got != "c,b,a" {
		t.Fatalf("pins = %s, want c,b,a", got)
	}
	pins, _ := r.Pins(e.ID)
	body, err := r.PinBody(e.ID, pins[0].ID)
	if err != nil || body != "body of c" {
		t.Fatalf("PinBody = %q, %v", body, err)
	}
	if pins[0].BodyLen != len("body of c") || pins[0].CreatedAt == 0 {
		t.Errorf("header = %+v", pins[0])
	}
	if _, err := os.Stat(filepath.Join(r.pinsDir(e.ID), pins[0].ID+".json")); err != nil {
		t.Errorf("header not on disk: %v", err)
	}
	if got := r.Get(e.ID).Info().PinCount; got != 3 {
		t.Errorf("Info().PinCount = %d, want 3", got)
	}
}

func TestAddPinRejectsInvalid(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	cases := []struct {
		name, title, body string
		want              error
	}{
		{"empty title", "  ", "b", ErrPinInvalid},
		{"empty body", "t", "\n", ErrPinInvalid},
		{"bad utf8", "t\xff", "b", ErrPinInvalid},
		{"long title", strings.Repeat("x", wire.MaxPinTitleLen+1), "b", ErrPinTooLong},
		{"long body", "t", strings.Repeat("x", wire.MaxPinBodyLen+1), ErrPinTooLong},
	}
	for _, c := range cases {
		if _, err := r.AddPin(e.ID, c.title, c.body); !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
	if ents, _ := os.ReadDir(r.pinsDir(e.ID)); len(ents) != 0 {
		t.Errorf("rejected pins left %d files", len(ents))
	}
	// Exactly at the bounds is fine.
	if _, err := r.AddPin(e.ID, strings.Repeat("x", wire.MaxPinTitleLen), strings.Repeat("y", wire.MaxPinBodyLen)); err != nil {
		t.Errorf("pin at the bounds: %v", err)
	}
}

func TestAddPinRefusedAtLimitNotEvicted(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	first := mustPin(t, r, e.ID, "first")
	for range wire.MaxPinsPerSession - 1 {
		mustPin(t, r, e.ID, "t")
	}
	if _, err := r.AddPin(e.ID, "over", "b"); !errors.Is(err, ErrPinLimit) {
		t.Fatalf("pin past the limit = %v, want ErrPinLimit", err)
	}
	// Refused, never an old one evicted to make room.
	if _, err := r.PinBody(e.ID, first.ID); err != nil {
		t.Fatalf("oldest pin gone: %v", err)
	}
	if err := r.Unpin(e.ID, first.ID); err != nil {
		t.Fatal(err)
	}
	mustPin(t, r, e.ID, "room again")
}

// A pin whose header cannot be removed still exists: Unpin says so and
// keeps it, rather than reporting a removal a restart would undo.
func TestUnpinKeepsPinWhenItCannotBeRemoved(t *testing.T) {
	skipNonPosix(t)
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	r := freshRegistry(t)
	e := createShell(t, r)
	a := mustPin(t, r, e.ID, "a")
	mustPin(t, r, e.ID, "b")
	dir := r.pinsDir(e.ID)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if err := r.Unpin(e.ID, a.ID); err == nil {
		t.Fatal("Unpin reported success with the header still on disk")
	}
	if err := r.UnpinAll(e.ID); err == nil {
		t.Fatal("UnpinAll reported success with headers still on disk")
	}
	if got := pinTitles(t, r, e.ID); len(got) != 2 {
		t.Fatalf("pins after failed removals = %v, want both kept", got)
	}
	_ = os.Chmod(dir, 0o700)
	if err := r.UnpinAll(e.ID); err != nil {
		t.Fatalf("UnpinAll once removable: %v", err)
	}
}

// A close that lands right after a reopen keeps its undo record and the
// pins beside it: the reopen's cleanup must not delete what that close
// just wrote. The hook closes the session at the first moment another
// caller can see it.
func TestCloseRightAfterReopenKeepsTombstoneAndPins(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id := e.ID
	mustPin(t, r, id, "keep")
	if err := r.Kill(id, true); err != nil {
		t.Fatal(err)
	}
	killed := make(chan error, 1)
	testHookRestoreVisible = func(sid string) {
		go func() { killed <- r.Kill(sid, true) }()
		// Let the close run ahead of the rest of the restore, without
		// deadlocking a restore that still holds the pins' lock.
		select {
		case err := <-killed:
			killed <- err
		case <-time.After(time.Second):
		}
	}
	t.Cleanup(func() { testHookRestoreVisible = nil })
	_, _, _ = r.Restore(id, session.Options{})
	select {
	case err := <-killed:
		if err != nil {
			t.Fatalf("Kill: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the close never finished")
	}
	if _, err := r.readTombstone(id); err != nil {
		t.Fatalf("the second close's tombstone is gone: %v", err)
	}
	if h := loadPinHeaders(pinRecordDir(ClosedDir(r.stateDir), id)); len(h) != 1 {
		t.Fatalf("pins beside the tombstone = %d, want 1", len(h))
	}
}

func TestAddPinUnknownSession(t *testing.T) {
	r := freshRegistry(t)
	if _, err := r.AddPin("nope", "t", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestPinsSurviveRegistryReload(t *testing.T) {
	skipNonPosix(t)
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	e := createShell(t, r)
	id := e.ID
	mustPin(t, r, id, "one")
	mustPin(t, r, id, "two")
	_ = r.Close()

	r2, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r2.Close() })
	if got := strings.Join(pinTitles(t, r2, id), ","); got != "two,one" {
		t.Fatalf("after reload pins = %s", got)
	}
	if got := r2.Get(id).Info().PinCount; got != 2 {
		t.Errorf("PinCount after reload = %d", got)
	}
	// A new pin after reload still sorts first.
	mustPin(t, r2, id, "three")
	if got := pinTitles(t, r2, id)[0]; got != "three" {
		t.Errorf("newest after reload = %s", got)
	}
}

func TestLoadReadsHeadersOnlyAndSweepsHalfPins(t *testing.T) {
	dir := t.TempDir()
	good := wire.Pin{ID: "0000000000000001aaaaaaaa", Title: "t", BodyLen: 1}
	if err := writeJSON(filepath.Join(dir, good.ID+".json"), good); err != nil {
		t.Fatal(err)
	}
	_ = os.WriteFile(filepath.Join(dir, good.ID+".md"), []byte("b"), 0o600)
	orphan := filepath.Join(dir, "0000000000000002bbbbbbbb.md")
	_ = os.WriteFile(orphan, []byte("half"), 0o600)
	h := loadPinHeaders(dir)
	if len(h) != 1 || h[0] != good {
		t.Fatalf("headers = %+v", h)
	}
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("body without a header was not swept")
	}
}

func TestPinsSurviveRestart(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	mustPin(t, r, e.ID, "keep")
	if err := r.Restart(e.ID); err != nil {
		t.Fatalf("Restart: %v", err)
	}
	if got := pinTitles(t, r, e.ID); len(got) != 1 || got[0] != "keep" {
		t.Fatalf("pins after restart = %v", got)
	}
}

func TestUnpinRemovesOnlyThatPin(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	a := mustPin(t, r, e.ID, "a")
	b := mustPin(t, r, e.ID, "b")
	if err := r.Unpin(e.ID, a.ID); err != nil {
		t.Fatalf("Unpin: %v", err)
	}
	if got := pinTitles(t, r, e.ID); len(got) != 1 || got[0] != "b" {
		t.Fatalf("pins = %v", got)
	}
	if _, err := os.Stat(filepath.Join(r.pinsDir(e.ID), a.ID+".md")); !os.IsNotExist(err) {
		t.Error("unpinned body still on disk")
	}
	if err := r.Unpin(e.ID, a.ID); !errors.Is(err, ErrNoSuchPin) {
		t.Errorf("second Unpin = %v", err)
	}
	if _, err := r.PinBody(e.ID, b.ID); err != nil {
		t.Errorf("survivor body: %v", err)
	}
}

func TestUnpinAllClears(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	mustPin(t, r, e.ID, "a")
	mustPin(t, r, e.ID, "b")
	if err := r.UnpinAll(e.ID); err != nil {
		t.Fatal(err)
	}
	if got := pinTitles(t, r, e.ID); len(got) != 0 {
		t.Fatalf("pins = %v", got)
	}
	if ents, _ := os.ReadDir(r.pinsDir(e.ID)); len(ents) != 0 {
		t.Errorf("%d files left after UnpinAll", len(ents))
	}
	if got := r.Get(e.ID).Info().PinCount; got != 0 {
		t.Errorf("PinCount = %d", got)
	}
}

// UnpinAll stopping part-way keeps the pins it could not remove and
// announces only the ones it did, never a clear.
func TestUnpinAllPartialFailureAnnouncesOnlyRemoved(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	mustPin(t, r, e.ID, "old")
	mid := mustPin(t, r, e.ID, "mid")
	newest := mustPin(t, r, e.ID, "new")
	// A non-empty directory where mid's header was: os.Remove fails.
	hdr := filepath.Join(r.pinsDir(e.ID), mid.ID+".json")
	if err := os.Remove(hdr); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(hdr, "x"), 0o700); err != nil {
		t.Fatal(err)
	}
	pl, unsub := r.SubscribePins()
	defer unsub()

	if err := r.UnpinAll(e.ID); err == nil {
		t.Fatal("UnpinAll reported success with a header it could not remove")
	}
	if got := pinTitles(t, r, e.ID); len(got) != 2 || got[0] != "mid" || got[1] != "old" {
		t.Fatalf("pins = %v, want [mid old]", got)
	}
	if got := r.Get(e.ID).Info().PinCount; got != 2 {
		t.Errorf("PinCount = %d, want 2", got)
	}
	select {
	case m := <-pl:
		if m.Cleared || len(m.RemovedIDs) != 1 || m.RemovedIDs[0] != newest.ID {
			t.Fatalf("delta = %+v, want RemovedIDs [%s]", m, newest.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no delta for the pin that was removed")
	}
}

func TestUnpinOtherSessionsPinIsNotFound(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	a := createShell(t, r)
	b := createShell(t, r)
	pa := mustPin(t, r, a.ID, "a's")
	if err := r.Unpin(b.ID, pa.ID); !errors.Is(err, ErrNoSuchPin) {
		t.Errorf("Unpin via other session = %v", err)
	}
	if _, err := r.PinBody(b.ID, pa.ID); !errors.Is(err, ErrNoSuchPin) {
		t.Errorf("PinBody via other session = %v", err)
	}
	if got := pinTitles(t, r, a.ID); len(got) != 1 {
		t.Errorf("a's pins = %v", got)
	}
	// A path-shaped id never reaches the filesystem.
	if _, err := r.PinBody(a.ID, "../../session"); !errors.Is(err, ErrNoSuchPin) {
		t.Errorf("traversal id = %v", err)
	}
}

func TestPinsFollowCloseAndReopen(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id := e.ID
	mustPin(t, r, id, "x")
	if err := r.Kill(id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pinRecordDir(ClosedDir(r.stateDir), id)); err != nil {
		t.Fatalf("pins not beside the tombstone: %v", err)
	}
	if _, _, err := r.Restore(id, session.Options{}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	if got := pinTitles(t, r, id); len(got) != 1 || got[0] != "x" {
		t.Fatalf("pins after reopen = %v", got)
	}
	if got := r.Get(id).Info().PinCount; got != 1 {
		t.Errorf("PinCount after reopen = %d", got)
	}
	pins, _ := r.Pins(id)
	if _, err := r.PinBody(id, pins[0].ID); err != nil {
		t.Errorf("body after reopen: %v", err)
	}
	if _, err := os.Stat(pinRecordDir(ClosedDir(r.stateDir), id)); !os.IsNotExist(err) {
		t.Error("pins left beside a consumed tombstone")
	}
}

func TestPinsFollowArchiveAndUnarchive(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id := e.ID
	mustPin(t, r, id, "x")
	if err := r.Archive(id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(pinRecordDir(ArchivedDir(r.stateDir), id)); err != nil {
		t.Fatalf("pins not beside the archive record: %v", err)
	}
	if _, _, err := r.Unarchive(id, session.Options{}); err != nil {
		t.Fatalf("Unarchive: %v", err)
	}
	if got := pinTitles(t, r, id); len(got) != 1 || got[0] != "x" {
		t.Fatalf("pins after unarchive = %v", got)
	}
}

func TestDiscardRecordRemovesPins(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	closed := createShell(t, r)
	archived := createShell(t, r)
	mustPin(t, r, closed.ID, "c")
	mustPin(t, r, archived.ID, "a")
	if err := r.Kill(closed.ID, true); err != nil {
		t.Fatal(err)
	}
	if err := r.Archive(archived.ID); err != nil {
		t.Fatal(err)
	}
	r.discardTombstone(closed.ID)
	if _, err := r.DeleteArchived(archived.ID, false, true); err != nil {
		t.Fatalf("DeleteArchived: %v", err)
	}
	for _, d := range []string{
		pinRecordDir(ClosedDir(r.stateDir), closed.ID),
		pinRecordDir(ArchivedDir(r.stateDir), archived.ID),
	} {
		if _, err := os.Stat(d); !os.IsNotExist(err) {
			t.Errorf("%s survived its record", d)
		}
	}
}

func TestStartupSweepsOrphanPinsDirs(t *testing.T) {
	t.Setenv("SHELL", "/bin/sh")
	dir := t.TempDir()
	orphan := pinRecordDir(ClosedDir(dir), "gone")
	kept := pinRecordDir(ClosedDir(dir), "kept")
	for _, d := range []string{orphan, kept} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeJSON(recordPath(ClosedDir(dir), "kept"), Tombstone{}); err != nil {
		t.Fatal(err)
	}
	r, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	if _, err := os.Stat(orphan); !os.IsNotExist(err) {
		t.Error("orphan pins dir not swept")
	}
	if _, err := os.Stat(kept); err != nil {
		t.Errorf("pins dir with a record was swept: %v", err)
	}
}

func TestKillRefusedKeepsPinning(t *testing.T) {
	skipNonPosix(t)
	r, p := freshRegistryWithProject(t)
	e := archivedWorktreeSession(t, r, p)
	mustWriteFile(t, filepath.Join(e.WorktreePath, "wip.txt"), "unfinished\n")
	if err := r.Kill(e.ID, false); !errors.Is(err, ErrWorktreeDirty) {
		t.Fatalf("Kill = %v, want ErrWorktreeDirty", err)
	}
	if _, err := r.AddPin(e.ID, "t", "b"); err != nil {
		t.Fatalf("AddPin after a refused kill: %v", err)
	}
}

// A pin racing a close must never recreate the removed session
// directory, nor be lost silently: it either lands before the close
// (and travels with the tombstone) or is refused.
func TestAddPinConcurrentWithKillLeavesNoOrphanDir(t *testing.T) {
	skipNonPosix(t)
	for range 10 {
		r := freshRegistry(t)
		e := createShell(t, r)
		id := e.ID
		var wg sync.WaitGroup
		var added []string
		var mu sync.Mutex
		for i := range 8 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				if p, err := r.AddPin(id, "t", "b"); err == nil {
					mu.Lock()
					added = append(added, p.ID)
					mu.Unlock()
				} else if !errors.Is(err, ErrNotFound) {
					t.Errorf("AddPin %d: %v", i, err)
				}
			}()
		}
		done := make(chan struct{})
		go func() {
			_ = r.Kill(id, true)
			wg.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Fatal("AddPin and Kill deadlocked")
		}
		if _, err := os.Stat(filepath.Join(SessionsDir(r.stateDir), id)); !os.IsNotExist(err) {
			t.Fatal("a pin recreated the killed session's directory")
		}
		kept := loadPinHeaders(pinRecordDir(ClosedDir(r.stateDir), id))
		if len(kept) != len(added) {
			t.Fatalf("%d pins accepted but %d kept with the tombstone", len(added), len(kept))
		}
	}
}

// The race above, made deterministic: an AddPin that looked the entry up
// before the close committed is refused, and writes nothing.
func TestAddPinOnClosedEntryIsRefused(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	if err := r.Kill(e.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err := r.addPin(e, "late", "b"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("addPin on a closed entry = %v, want ErrNotFound", err)
	}
	if _, err := os.Stat(filepath.Join(SessionsDir(r.stateDir), e.ID)); !os.IsNotExist(err) {
		t.Fatal("a late pin recreated the killed session's directory")
	}
}

func TestUnpinConcurrentWithArchive(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	id := e.ID
	pins := []wire.Pin{mustPin(t, r, id, "a"), mustPin(t, r, id, "b")}
	done := make(chan struct{})
	go func() {
		var wg sync.WaitGroup
		for _, p := range pins {
			wg.Add(1)
			go func() { defer wg.Done(); _ = r.Unpin(id, p.ID) }()
		}
		_ = r.Archive(id)
		wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("Unpin and Archive deadlocked")
	}
	if _, err := os.Stat(filepath.Join(SessionsDir(r.stateDir), id)); !os.IsNotExist(err) {
		t.Fatal("session directory survived its archive")
	}
}

func TestPinsChangedBroadcastAndSessionUpdate(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	e := createShell(t, r)
	pl, unsubP := r.SubscribePins()
	defer unsubP()
	sl, unsubS := r.Subscribe()
	defer unsubS()
	p := mustPin(t, r, e.ID, "a")
	select {
	case m := <-pl:
		if m.SessionID != e.ID || m.Added == nil || m.Added.ID != p.ID {
			t.Fatalf("delta = %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no PINS_CHANGED")
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case ev := <-sl:
			if ev.Kind == wire.SessionEventUpdated && ev.Session.ID == e.ID && ev.Session.PinCount == 1 {
				goto cleared
			}
		case <-deadline:
			t.Fatal("no session update carrying pin_count")
		}
	}
cleared:
	if err := r.UnpinAll(e.ID); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-pl:
		if !m.Cleared {
			t.Fatalf("delta = %+v", m)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no clear delta")
	}
}

func TestLoadKeepsBodyOfUnreadableHeader(t *testing.T) {
	dir := t.TempDir()
	id := "0000000000000003cccccccc"
	_ = os.WriteFile(filepath.Join(dir, id+".json"), []byte("{not json"), 0o600)
	body := filepath.Join(dir, id+".md")
	_ = os.WriteFile(body, []byte("keep me"), 0o600)
	if h := loadPinHeaders(dir); len(h) != 0 {
		t.Fatalf("headers = %+v", h)
	}
	if _, err := os.Stat(body); err != nil {
		t.Errorf("body of an unreadable header was deleted: %v", err)
	}
}
