package session

import (
	"sync"
	"testing"
	"time"
)

// newBareSession builds a Session with a VT but no PTY. Every title path
// under test is driven by deliver + noteTitle, both of which only touch
// the VT and the hook, so a real child process would add a shell
// dependency and a few hundred milliseconds for nothing.
func newBareSession() *Session {
	return &Session{
		ID:    "title-test",
		vt:    NewVT(80, 24),
		sinks: make(map[Sink]struct{}),
		done:  make(chan struct{}),
	}
}

// feed pushes bytes through the same two calls readLoop makes.
func (s *Session) feed(b string) {
	s.deliver([]byte(b))
	s.noteTitle()
}

type titleRecorder struct {
	mu   sync.Mutex
	seen []string
}

func (r *titleRecorder) hook(t string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = append(r.seen, t)
}

func (r *titleRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.seen...)
}

func TestVTTitleFromOSC(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no osc", "plain output\r\n", ""},
		{"osc 0 bel", "\x1b]0;hello\x07", "hello"},
		{"osc 2 bel", "\x1b]2;window\x07", "window"},
		{"osc 0 st", "\x1b]0;via-st\x1b\\", "via-st"},
		{"last one wins", "\x1b]0;first\x07text\x1b]0;second\x07", "second"},
		// vt10x ignores an empty OSC title (`if title != ""` in
		// str.go's handleSTR), so a program cannot clear its title back
		// to nothing — the last non-empty value sticks. Asserted rather
		// than worked around: the alternative is forking the OSC parser.
		{"empty osc does not clear", "\x1b]0;something\x07\x1b]0;\x07", "something"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := NewVT(80, 24)
			if _, err := v.Write([]byte(tc.input)); err != nil {
				t.Fatalf("write: %v", err)
			}
			if got := v.Title(); got != tc.want {
				t.Errorf("Title() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestSessionTitleHookFiresOnChange(t *testing.T) {
	s := newBareSession()
	rec := &titleRecorder{}
	s.SetTitleHook(rec.hook)

	s.feed("\x1b]0;building\x07")

	if got := s.Title(); got != "building" {
		t.Fatalf("Title() = %q, want %q", got, "building")
	}
	if got := rec.snapshot(); len(got) != 1 || got[0] != "building" {
		t.Fatalf("hook calls = %q, want [building]", got)
	}
}

// A TUI that repaints its title every frame with the same string must
// cost nothing downstream — this is the guard that keeps the sidebar
// from being rebuilt at the child process's frame rate.
func TestSessionTitleHookSkipsUnchanged(t *testing.T) {
	s := newBareSession()
	rec := &titleRecorder{}
	s.SetTitleHook(rec.hook)

	for range 5 {
		s.feed("\x1b]0;idle\x07")
	}

	if got := rec.snapshot(); len(got) != 1 {
		t.Fatalf("hook calls = %q, want exactly one", got)
	}
}

func TestSessionTitleHookNotSetIsNoop(t *testing.T) {
	s := newBareSession()
	// No hook installed: must not panic, and must still track the title
	// so a hook installed later reports the next *change*.
	s.feed("\x1b]0;quiet\x07")
	if got := s.Title(); got != "quiet" {
		t.Fatalf("Title() = %q, want %q", got, "quiet")
	}
}

// A burst of distinct titles collapses to one immediate report plus one
// trailing report carrying the final value — never a report per frame,
// and never a silently dropped final state.
func TestSessionTitleThrottleCoalescesAndTrails(t *testing.T) {
	prev := titleThrottle
	titleThrottle = 30 * time.Millisecond
	t.Cleanup(func() { titleThrottle = prev })

	s := newBareSession()
	rec := &titleRecorder{}
	s.SetTitleHook(rec.hook)

	for _, frame := range []string{"step 1", "step 2", "step 3", "step 4"} {
		s.feed("\x1b]0;" + frame + "\x07")
	}

	// Immediately: only the first title has been reported; the rest are
	// waiting behind the throttle.
	if got := rec.snapshot(); len(got) != 1 || got[0] != "step 1" {
		t.Fatalf("during burst hook calls = %q, want [step 1]", got)
	}

	// After the window closes the trailing fire delivers the final title.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := rec.snapshot()
		if len(got) >= 2 && got[len(got)-1] == "step 4" {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("trailing title never arrived; hook calls = %q", rec.snapshot())
}

// The trailing timer must not outlive the PTY: fanoutClose stops it, so
// a closed session goes quiet instead of firing one more time.
func TestSessionTitleTimerStoppedOnClose(t *testing.T) {
	prev := titleThrottle
	titleThrottle = 30 * time.Millisecond
	t.Cleanup(func() { titleThrottle = prev })

	s := newBareSession()
	rec := &titleRecorder{}
	s.SetTitleHook(rec.hook)

	s.feed("\x1b]0;first\x07")  // reports immediately, arms the timer
	s.feed("\x1b]0;second\x07") // queued behind the throttle
	s.fanoutClose()

	time.Sleep(4 * titleThrottle)
	if got := rec.snapshot(); len(got) != 1 {
		t.Fatalf("hook calls after close = %q, want only the pre-close one", got)
	}
}
