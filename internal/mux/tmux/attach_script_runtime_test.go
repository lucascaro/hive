package muxtmux

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/mux"
)

// TestAttachScript_TrapBodyIsExecutable regression-guards against the class
// of bugs where the trap body parses as a string at script-entry time (so
// `sh -n` passes) but throws a runtime syntax error when the trap actually
// fires on detach.
//
// We cannot run the real attach script end-to-end (it shells out to tmux),
// but we can isolate the trap body and run it directly to prove every
// statement inside it is a valid complete shell command. The specific
// failure this catches: splitting `if / then / else / fi` across multiple
// entries in the restore-lines slice, which produces `; then;` after
// `strings.Join(..., "; ")`. That was a P0 bug caught during review —
// without this test, `go test ./...` was green but detach would have left
// the terminal in alt-screen and leaked the tmux root-table bind-key.
func TestAttachScript_TrapBodyIsExecutable(t *testing.T) {
	spec, err := mux.ParseDetachKey("ctrl+q")
	if err != nil {
		t.Fatal(err)
	}
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "demo · claude", spec)

	// Extract the trap body. The script contains exactly one line of the
	// form `trap '<body>' EXIT INT TERM HUP`.
	const trapStart = "trap '"
	i := strings.Index(script, trapStart)
	if i < 0 {
		t.Fatalf("no trap line in script:\n%s", script)
	}
	rest := script[i+len(trapStart):]
	j := strings.Index(rest, "' EXIT")
	if j < 0 {
		t.Fatalf("trap line missing closing quote before EXIT:\n%s", script)
	}
	body := rest[:j]

	// Replace external tmux calls with no-op `:` so the body runs in a
	// sandbox without touching a real tmux server. We also set the
	// referenced variables to plausible values so the if/then branches
	// actually execute rather than short-circuit.
	harness := `
set -e
tmux() { :; }
old_detach=""
had_status=0
had_status_position=0
had_status_style=0
had_status_left=0
had_status_right=0
had_status_left_length=0
had_status_right_length=0
had_window_status_format=0
had_window_status_current_format=0
had_window_status_separator=0
` + body

	out, err := exec.Command("sh", "-c", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("trap body failed to execute under sh: %v\n--- output ---\n%s\n--- body ---\n%s", err, out, body)
	}

	// Also verify under bash, which has slightly different parsing rules.
	out, err = exec.Command("bash", "-c", harness).CombinedOutput()
	if err != nil {
		t.Fatalf("trap body failed to execute under bash: %v\n--- output ---\n%s\n--- body ---\n%s", err, out, body)
	}
}
