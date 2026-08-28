package registry

import (
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

// End to end: a program that sets its window title produces a
// SESSION_EVENT(updated) carrying that title, without any client
// having attached to the session.
func TestTitleChangeBroadcastsUpdated(t *testing.T) {
	skipOnWindows(t)
	r := freshRegistry(t)
	e := mustCreate(t, r, wire.CreateSpec{Name: "titled"})

	listener, unsub := r.Subscribe()
	defer unsub()
	drain(listener)

	sess := r.entries[e.ID].sess
	if sess == nil {
		t.Fatal("created session has no live process")
	}
	if _, err := sess.Write([]byte("printf '\\033]0;deploying\\007'\n")); err != nil {
		t.Fatalf("write to pty: %v", err)
	}

	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-listener:
			if ev.Kind == wire.SessionEventUpdated &&
				ev.Session.ID == e.ID &&
				strings.Contains(ev.Session.Title, "deploying") {
				return
			}
		case <-deadline:
			t.Fatal("no updated event carrying the window title arrived")
		}
	}
}
