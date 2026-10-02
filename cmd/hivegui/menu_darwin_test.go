//go:build darwin

package main

import (
	"encoding/json"
	"os"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
)

// findItem walks the menu tree for the first item with the given label.
func findItem(items []*menu.MenuItem, label string) *menu.MenuItem {
	for _, it := range items {
		if it.Label == label {
			return it
		}
		if it.SubMenu != nil {
			if found := findItem(it.SubMenu.Items, label); found != nil {
				return found
			}
		}
	}
	return nil
}

// TestSessionMenuAttentionItems pins the ⌘B / ⇧⌘B menu entries.
//
// The menu reaches the frontend by emitting bare event-name strings that
// app/commands.ts registers in its MENU_COMMANDS map — a contract no
// compiler checks. Renaming one side leaves the menu item wired to
// nothing, and the app still builds and runs. The JS mirror of this
// assertion lives in frontend/test/dom/attention-jump.test.ts
// ("menu action ids").
func TestSessionMenuAttentionItems(t *testing.T) {
	m := buildAppMenu(&App{})
	if m == nil {
		t.Fatal("buildAppMenu returned nil on darwin")
	}

	for _, tc := range []struct {
		label     string
		wantShift bool
	}{
		{"Next Session Needing Attention", false},
		{"Jump Back to Where You Were", true},
	} {
		item := findItem(m.Items, tc.label)
		if item == nil {
			t.Errorf("menu item %q not found", tc.label)
			continue
		}
		if item.Accelerator == nil {
			t.Errorf("%q has no accelerator", tc.label)
			continue
		}
		if item.Accelerator.Key != "b" {
			t.Errorf("%q bound to %q, want \"b\"", tc.label, item.Accelerator.Key)
		}
		var hasShift, hasCmd bool
		for _, mod := range item.Accelerator.Modifiers {
			switch mod {
			case keys.ShiftKey:
				hasShift = true
			case keys.CmdOrCtrlKey:
				hasCmd = true
			}
		}
		if !hasCmd {
			t.Errorf("%q missing CmdOrCtrl modifier", tc.label)
		}
		if hasShift != tc.wantShift {
			t.Errorf("%q shift = %v, want %v", tc.label, hasShift, tc.wantShift)
		}
	}
}

// walkAccelerators collects every (label, accelerator) pair in the menu
// tree. findItem above searches by label and so can't express "no item
// binds this key", which is exactly the assertion below.
func walkAccelerators(items []*menu.MenuItem, out map[string]*keys.Accelerator) {
	for _, it := range items {
		if it.Accelerator != nil {
			out[it.Label] = it.Accelerator
		}
		if it.SubMenu != nil {
			walkAccelerators(it.SubMenu.Items, out)
		}
	}
}

// TestMenuHasNoEnterAccelerator keeps ⌘↩ out of the native menu (#249).
// Same mechanism as the arrow guard below: AppKit consumes a registered
// key equivalent before the webview sees a keydown, so a menu item here
// would toggle grid no matter what the frontend does — which is exactly
// how the "⌘Enter is unusable inside an agent session" bug worked. The
// Playwright specs cannot catch a re-add: they drive the browser mock,
// which has no native menu.
func TestMenuHasNoEnterAccelerator(t *testing.T) {
	m := buildAppMenu(&App{})
	if m == nil {
		t.Fatal("buildAppMenu returned nil on darwin")
	}
	accels := map[string]*keys.Accelerator{}
	walkAccelerators(m.Items, accels)
	for label, acc := range accels {
		if acc.Key == "enter" || acc.Key == "return" {
			t.Errorf("menu item %q binds %q; ⌘↩ must reach the terminal", label, acc.Key)
		}
	}
	// Guard the walker itself: ⌘G, the binding that replaced ⌘↩, must
	// still be found — otherwise this test passes on an empty walk.
	var g bool
	for _, acc := range accels {
		if acc.Key == "g" {
			g = true
		}
	}
	if !g {
		t.Error("walkAccelerators found no 'g' binding; the walker is broken")
	}
}

// TestMenuHasNoArrowLeftRightAccelerators keeps ⌘←/⌘→ (and their shifted
// forms) out of the native menu. AppKit consumes a registered key
// equivalent before the webview ever sees a keydown, so a menu item here
// steals start/end-of-line from the terminal no matter what the frontend
// does with the event.
func TestMenuHasNoArrowLeftRightAccelerators(t *testing.T) {
	m := buildAppMenu(&App{})
	if m == nil {
		t.Fatal("buildAppMenu returned nil on darwin")
	}
	accels := map[string]*keys.Accelerator{}
	walkAccelerators(m.Items, accels)
	for label, acc := range accels {
		if acc.Key == "left" || acc.Key == "right" {
			t.Errorf("menu item %q binds %q; ⌘←/⌘→ must reach the terminal", label, acc.Key)
		}
	}
	// Guard the walker itself: the vertical twins must still be there.
	var down bool
	for _, acc := range accels {
		if acc.Key == "down" {
			down = true
		}
	}
	if !down {
		t.Error("walkAccelerators found no 'down' binding; the walker is broken")
	}
}

// TestReopenClosedSessionAccelerator pins ⌘Z to Reopen Closed Session
// and, more importantly, proves nothing else claims it. ⇧⌘T was the
// obvious choice for this action and is already New Session in
// Worktree; this test is what stops the next binding from repeating
// that.
func TestReopenClosedSessionAccelerator(t *testing.T) {
	m := buildAppMenu(&App{})
	accels := map[string]*keys.Accelerator{}
	walkAccelerators(m.Items, accels)

	got, ok := accels["Reopen Closed Session"]
	if !ok || got == nil {
		t.Fatal("Reopen Closed Session has no accelerator")
	}
	if got.Key != "z" {
		t.Errorf("Reopen Closed Session bound to %q, want \"z\"", got.Key)
	}
	var cmd, shift bool
	for _, mod := range got.Modifiers {
		switch mod {
		case keys.CmdOrCtrlKey:
			cmd = true
		case keys.ShiftKey:
			shift = true
		}
	}
	if !cmd {
		t.Error("Reopen Closed Session missing CmdOrCtrl modifier")
	}
	if shift {
		t.Error("Reopen Closed Session is ⇧⌘Z; that is conventionally redo")
	}

	for label, a := range accels {
		if label == "Reopen Closed Session" || a == nil || a.Key != "z" {
			continue
		}
		hasShift := false
		for _, mod := range a.Modifiers {
			if mod == keys.ShiftKey {
				hasShift = true
			}
		}
		if !hasShift {
			t.Errorf("%q also claims ⌘Z", label)
		}
	}
}

// TestReloadAndRestartMenuItems pins the two items that now sit next to
// each other in File and differ enormously in cost.
//
// Neither carries an accelerator, for different reasons: Restart Daemon
// ends every running shell and agent, and Reload GUI would otherwise
// answer the ⌘R browser reflex by throwing away the user's window
// mid-agent-run for no benefit.
func TestReloadAndRestartMenuItems(t *testing.T) {
	m := buildAppMenu(&App{})
	if m == nil {
		t.Fatal("buildAppMenu returned nil on darwin")
	}

	for _, label := range []string{
		"Reload GUI",
		"Restart Daemon… (ends all sessions)",
	} {
		item := findItem(m.Items, label)
		if item == nil {
			t.Errorf("menu item %q not found", label)
			continue
		}
		if item.Accelerator != nil {
			t.Errorf("%q has accelerator %+v; both items are deliberately unbound",
				label, item.Accelerator)
		}
	}

	// The old label must be gone: it named the cheap action while doing
	// the expensive one, which is the confusion this split removes.
	if findItem(m.Items, "Restart Hive…") != nil {
		t.Error(`"Restart Hive…" still present; it was renamed to name its cost`)
	}
}

func readMenuFixture(t *testing.T) map[string]string {
	t.Helper()
	b, err := os.ReadFile("testdata/menu-default-accelerators.json")
	if err != nil {
		t.Fatal(err)
	}
	var want map[string]string
	if err := json.Unmarshal(b, &want); err != nil {
		t.Fatal(err)
	}
	return want
}

// TestMenuDefaultsMatchFixture ties the hard-coded menu defaults to the
// frontend's binding data (spec 477): the frontend asserts its derived
// defaults equal the same fixture (frontend/test/unit/bindings.test.ts).
// The keymap override sends only items whose accelerator differs from
// the default, so the two sides drifting would leave a rebound command's
// old chord live in the menu. Every item must go through accel(): one
// that skips it is missing from the recorded map.
func TestMenuDefaultsMatchFixture(t *testing.T) {
	got := map[string]string{}
	buildAppMenuRecorded(&App{}, got)
	if want := readMenuFixture(t); !reflect.DeepEqual(got, want) {
		t.Fatalf("menu accelerators drifted from testdata/menu-default-accelerators.json\ngot  %v\nwant %v", got, want)
	}
}

func TestFixtureAcceleratorsParse(t *testing.T) {
	for id, s := range readMenuFixture(t) {
		if s == "" {
			continue
		}
		acc, err := keys.Parse(s)
		if err != nil {
			t.Errorf("%s: %q does not parse: %v", id, s, err)
			continue
		}
		if back := accelString(acc); back != s {
			t.Errorf("%s: %q round-trips as %q", id, s, back)
		}
	}
}

// TestMenuAcceleratorOverride: the user's keymap replaces an item's
// accelerator, "" removes it, and a string that will not parse removes
// it too — never the default, which would keep the old chord firing.
func TestMenuAcceleratorOverride(t *testing.T) {
	a := &App{menuAccel: map[string]string{
		"new-session":   "cmdorctrl+y",
		"worktrees":     "cmdorctrl+e", // no menu item: ignored
		"close-session": "",
		"settings":      "cmdorctrl+nope",
	}}
	got := map[string]string{}
	m := buildAppMenuRecorded(a, got)
	if acc := findItem(m.Items, "New Session").Accelerator; acc == nil || acc.Key != "y" {
		t.Fatalf("New Session accelerator = %+v, want ⌘Y", acc)
	}
	if acc := findItem(m.Items, "Close Session").Accelerator; acc != nil {
		t.Fatalf("Close Session accelerator = %+v, want none", acc)
	}
	if acc := findItem(m.Items, "Settings…").Accelerator; acc != nil {
		t.Fatalf("Settings accelerator = %+v, want none for an unparseable override", acc)
	}
	want := readMenuFixture(t)
	want["new-session"] = "cmdorctrl+y"
	want["close-session"] = ""
	want["settings"] = ""
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("other items must keep their defaults\ngot  %v\nwant %v", got, want)
	}
}

// TestMenuRebuildsInstallInOrder: SetDebugTrace and SetMenuAccelerators
// run on Wails' goroutines and both fire at boot. Whatever order they land
// in, the menu installed LAST must be built from the final fields — a
// stale build installed after a newer one would put the old shortcuts
// back. Drives the real install path (via menuInstaller); run with -race.
func TestMenuRebuildsInstallInOrder(t *testing.T) {
	var mu sync.Mutex
	var installed []*menu.Menu
	a := &App{}
	a.menuInstaller = func(m *menu.Menu) {
		// Yield first, so a build that is not installed under the lock
		// gets overtaken by a newer one.
		time.Sleep(50 * time.Microsecond)
		mu.Lock()
		installed = append(installed, m)
		mu.Unlock()
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			a.SetMenuAccelerators(map[string]string{"new-session": "cmdorctrl+" + string(rune('a'+i))})
		}(i)
		go func(i int) {
			defer wg.Done()
			a.SetDebugTrace(i%2 == 0)
		}(i)
	}
	wg.Wait()
	if len(installed) != 32 {
		t.Fatalf("installed %d menus, want 32", len(installed))
	}
	last := installed[len(installed)-1]
	want := a.menuAccel["new-session"]
	if got := accelString(findItem(last.Items, "New Session").Accelerator); got != want {
		t.Fatalf("last installed menu has New Session = %q, want the final %q", got, want)
	}
	wantTrace := "Turn Debug Trace On (Reloads)"
	if a.debugTrace {
		wantTrace = "Turn Debug Trace Off (Reloads)"
	}
	if findItem(last.Items, wantTrace) == nil {
		t.Fatalf("last installed menu does not reflect the final debugTrace=%v", a.debugTrace)
	}
}

// TestSuspendMenuAccelerators: while a Settings › Shortcuts capture field
// has focus every item Hive builds loses its accelerator, so ⌘ chords
// reach the webview; lifting the suspension restores the user's keymap,
// and a new accelerator set (a fresh page's first push) lifts it too.
func TestSuspendMenuAccelerators(t *testing.T) {
	var last *menu.Menu
	a := &App{menuInstaller: func(m *menu.Menu) { last = m }}
	a.SetMenuAccelerators(map[string]string{"new-session": "cmdorctrl+y"})

	a.SuspendMenuAccelerators(true)
	got := map[string]string{}
	buildAppMenuRecorded(a, got)
	for id, acc := range got {
		if acc != "" {
			t.Errorf("suspended: %s keeps accelerator %q", id, acc)
		}
	}
	if acc := findItem(last.Items, "New Session").Accelerator; acc != nil {
		t.Fatalf("installed menu still has New Session = %+v", acc)
	}

	a.SuspendMenuAccelerators(false)
	if got := accelString(findItem(last.Items, "New Session").Accelerator); got != "cmdorctrl+y" {
		t.Fatalf("after lifting, New Session = %q, want the user's cmdorctrl+y", got)
	}

	a.SuspendMenuAccelerators(true)
	a.SetMenuAccelerators(map[string]string{})
	if got := accelString(findItem(last.Items, "New Session").Accelerator); got != "cmdorctrl+t" {
		t.Fatalf("a new accelerator set must lift the suspension; New Session = %q", got)
	}
}
