package registry

import (
	"context"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/agentstate"
	"github.com/lucascaro/hive/internal/wire"
)

// The state model is inferred from what an agent's TUI renders, and
// every bug in it so far has come from an assumption about that
// rendering that no unit test could contradict: Claude Code polls the
// terminal 5x a second without changing a cell, Pi renders on the alt
// screen. Neither is visible to a test that fabricates output.
//
// So this one runs the real binaries. It is opt-in — HIVE_PROBE_AGENTS=1
// — because it depends on what is installed on the machine, and it
// skips any agent that is not on PATH. Nothing here sends a prompt or
// touches the network: typing a few characters is enough to make an
// agent redraw, which is the whole question.
//
// Run it after touching anything in the state path:
//
//	HIVE_PROBE_AGENTS=1 go test ./internal/registry/ -run TestAgentTUIStateFlow -v
func TestAgentTUIStateFlow(t *testing.T) {
	skipNonPosix(t)
	if testing.Short() {
		t.Skip("spawns real agent binaries")
	}
	if os.Getenv("HIVE_PROBE_AGENTS") != "1" {
		t.Skip("set HIVE_PROBE_AGENTS=1 to run the real-agent state probe")
	}
	for _, id := range []agent.ID{agent.IDPi, agent.IDClaude} {
		def, ok := agent.Get(id)
		if !ok || len(def.Cmd) == 0 {
			continue
		}
		bin := def.Cmd[0]
		t.Run(string(id), func(t *testing.T) {
			if _, err := exec.LookPath(bin); err != nil {
				t.Skipf("%s not on PATH", bin)
			}
			r := freshRegistry(t)
			e, err := r.Create(context.Background(), wire.CreateSpec{
				Name: string(id), Cols: 120, Rows: 40,
				Agent: string(id), Cmd: def.Cmd,
			})
			if err != nil {
				t.Fatalf("Create: %v", err)
			}
			r.mu.Lock()
			ent, sess := r.entries[e.ID], r.entries[e.ID].sess
			r.mu.Unlock()
			if sess == nil {
				t.Fatal("no live process")
			}
			t.Cleanup(func() { _ = r.Kill(e.ID, true) })

			// 1. Starting up paints a UI: that is work.
			waitDigestChange(t, sess, 30*time.Second)
			now := time.Now()
			sample(r, ent, now)
			if got := r.Get(e.ID).Info().State; got != wire.StateWorking {
				t.Errorf("state = %q while %s was painting its UI, want %q",
					got, bin, wire.StateWorking)
			}

			// 2. Sitting at its prompt is not, however much it writes.
			// This is the Claude ESC[?6n case, live.
			settleFor(t, sess, 3*time.Second)
			sample(r, ent, time.Now())
			sample(r, ent, time.Now().Add(agentstate.QuietAfter))
			if got := r.Get(e.ID).Info().State; got != wire.StateIdle {
				t.Errorf("state = %q while %s sat idle at its prompt, want %q",
					got, bin, wire.StateIdle)
			}

			// 3. Typing redraws the input box: work again. No prompt is
			// submitted, so nothing is sent anywhere.
			if _, err := sess.Write([]byte("hello")); err != nil {
				t.Fatalf("write: %v", err)
			}
			waitDigestChange(t, sess, 15*time.Second)
			sample(r, ent, time.Now())
			if got := r.Get(e.ID).Info().State; got != wire.StateWorking {
				t.Errorf("state = %q after typing into %s, want %q",
					got, bin, wire.StateWorking)
			}
		})
	}
}

// waitDigestChange blocks until the session's rendered screen changes.
func waitDigestChange(t *testing.T, sess interface{ ScreenDigest() uint64 }, within time.Duration) {
	t.Helper()
	start := sess.ScreenDigest()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if sess.ScreenDigest() != start {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("screen never changed within %s", within)
}

// settleFor waits until the screen has been still for d, so the next
// sample is measuring a genuinely idle agent rather than a slow one.
func settleFor(t *testing.T, sess interface{ ScreenDigest() uint64 }, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	last := sess.ScreenDigest()
	still := time.Now()
	for time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
		cur := sess.ScreenDigest()
		if cur != last {
			last, still = cur, time.Now()
			continue
		}
		if time.Since(still) >= d {
			return
		}
	}
	t.Fatalf("agent never stopped redrawing for %s", d)
}
