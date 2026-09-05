package registry

import (
	"context"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/lucascaro/hive/internal/wire"
)

func TestTruncateTitle(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantLen int
		want    string
	}{
		{name: "short passes through", in: "npm run build", want: "npm run build"},
		{
			name:    "long is capped",
			in:      strings.Repeat("a", wire.MaxTitleLen+50),
			wantLen: wire.MaxTitleLen,
		},
		{
			// A cut landing mid-rune must not leave invalid UTF-8 on the
			// wire — json.Marshal would silently replace it.
			name:    "multibyte cut drops the partial rune",
			in:      strings.Repeat("a", wire.MaxTitleLen-1) + "€",
			wantLen: wire.MaxTitleLen - 1,
			want:    strings.Repeat("a", wire.MaxTitleLen-1),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := truncateTitle(tc.in)
			if tc.want != "" && got != tc.want {
				t.Errorf("truncateTitle() = %q, want %q", got, tc.want)
			}
			if tc.wantLen != 0 && len(got) != tc.wantLen {
				t.Errorf("len = %d, want %d", len(got), tc.wantLen)
			}
			if !utf8.ValidString(got) {
				t.Errorf("result is not valid UTF-8: %q", got)
			}
		})
	}
}

// An entry with no live process reports no title. This is the whole
// reason the title is read through to the session instead of mirrored
// into a field: death, restart and daemon boot need no clearing code.
func TestInfoTitleEmptyWithoutSession(t *testing.T) {
	e := &Entry{ID: "x", Name: "n"}
	if got := e.Info().Title; got != "" {
		t.Errorf("Title = %q, want empty for an entry with no session", got)
	}
}

// noteTitleChange is the registry half of the plumbing: it must announce
// the entry under SessionEventTitle, carrying the session's current
// title. Driven directly rather than through a PTY so the assertion does
// not depend on what any particular shell decides to title itself —
// internal/session/title_test.go already covers OSC bytes reaching
// Title(), and this covers what the registry does with that.
func TestNoteTitleChangeUsesTheTitleKind(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "titled"})

	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	r.noteTitleChange(e.ID)

	deadline := time.After(5 * time.Second)
	for {
		select {
		case ev := <-listener:
			// The live shell keeps producing output, so state events
			// interleave; this assertion is about the title kind only.
			if ev.Kind == wire.SessionEventState {
				continue
			}
			if ev.Kind != wire.SessionEventTitle {
				t.Fatalf("kind = %q, want %q — sharing %q is what made the "+
					"event stream nondeterministic for other consumers",
					ev.Kind, wire.SessionEventTitle, wire.SessionEventUpdated)
			}
			if ev.Session.ID != e.ID {
				t.Errorf("session id = %q, want %q", ev.Session.ID, e.ID)
			}
			return
		case <-deadline:
			t.Fatal("noteTitleChange broadcast nothing")
		}
	}
}

// An entry with no live session must stay silent: there is no title to
// report, and a broadcast would tell clients to re-read a dead session.
func TestNoteTitleChangeIgnoresADeadEntry(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "titled"})

	r.mu.Lock()
	r.entries[e.ID].sess = nil
	r.mu.Unlock()

	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	r.noteTitleChange(e.ID)
	r.noteTitleChange("no-such-id")

	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-listener:
			// The shell is still running and still writing to its PTY,
			// so its state keeps moving; only title events are on
			// trial here.
			if ev.Kind == wire.SessionEventState {
				continue
			}
			t.Fatalf("unexpected %s event for %s", ev.Kind, ev.Session.ID)
		case <-deadline:
			return
		}
	}
}

// End to end through a real PTY: the hook is actually installed at the
// session-assignment sites, and a title set by the program on the PTY
// reaches subscribers as SessionEventTitle carrying that title.
//
// The OSC is written on a retry loop rather than once. Cmd is run under
// a login shell (`zsh -l -i -c "cat"`), so a single early write can land
// before the shell has exec'd the command and be swallowed; re-sending
// until the title arrives makes the test independent of that startup
// race without weakening the assertion.
func TestTitleChangeBroadcastsTitleEvent(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e, err := r.Create(context.Background(), wire.CreateSpec{
		Name: "titled", Cols: 80, Rows: 24, Cmd: []string{"cat"},
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	sess := r.entries[e.ID].sess
	if sess == nil {
		t.Fatal("created session has no live process")
	}

	stop := make(chan struct{})
	defer close(stop)
	go func() {
		for {
			select {
			case <-stop:
				return
			default:
			}
			_, _ = sess.Write([]byte("\x1b]0;deploying\x07\n"))
			time.Sleep(200 * time.Millisecond)
		}
	}()

	deadline := time.After(20 * time.Second)
	for {
		select {
		case ev := <-listener:
			if ev.Session.ID != e.ID {
				continue
			}
			if ev.Kind == wire.SessionEventUpdated {
				t.Fatalf("title change arrived as %q; it must use %q",
					wire.SessionEventUpdated, wire.SessionEventTitle)
			}
			if ev.Kind == wire.SessionEventTitle &&
				strings.Contains(ev.Session.Title, "deploying") {
				return
			}
		case <-deadline:
			t.Fatalf("no title event carrying the window title arrived; "+
				"session title is now %q", sess.Title())
		}
	}
}
