package muxtmux

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/mux"
)

func TestBuildAttachScript_BindKeySaveAndRestore(t *testing.T) {
	spec, err := mux.ParseDetachKey("ctrl+q")
	if err != nil {
		t.Fatalf("ParseDetachKey: %v", err)
	}
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "demo · claude", spec)

	wantSubstrings := []string{
		// Save prior root binding in re-executable form so we can restore it.
		`old_detach=$(tmux list-keys -T root -aN 'C-q' 2>/dev/null)`,
		// Install our binding.
		`tmux bind-key -n C-q detach-client`,
		// Trap restores prior binding (eval) or unbinds (when none existed),
		// covering EXIT and signal-driven shutdowns so the binding does not
		// leak on the tmux server if hive is killed.
		`if [ -n "$old_detach" ]; then`,
		`eval "tmux $old_detach"`,
		`tmux unbind-key -n C-q`,
		`' EXIT INT TERM HUP`,
		// The detach-key display string is shown on the right of the status bar.
		`Ctrl+Q: detach`,
		// The actual attach call is still present.
		`tmux attach-session -t 'hive-sessions:0'`,
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(script, want) {
			t.Errorf("attach script missing substring %q\n--- script ---\n%s", want, script)
		}
	}
}

func TestBuildAttachScript_BindKeyBeforeAttach(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+q")
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "title", spec)

	bindIdx := strings.Index(script, "tmux bind-key -n C-q detach-client")
	attachIdx := strings.Index(script, "tmux attach-session")
	if bindIdx < 0 || attachIdx < 0 {
		t.Fatalf("expected both bind-key and attach-session in script:\n%s", script)
	}
	if bindIdx > attachIdx {
		t.Errorf("bind-key (%d) must run before attach-session (%d)", bindIdx, attachIdx)
	}
}

func TestBuildAttachScript_HonorsConfiguredKey(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+x")
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "title", spec)

	if !strings.Contains(script, "tmux bind-key -n C-x detach-client") {
		t.Errorf("expected bind-key for C-x, script:\n%s", script)
	}
	if !strings.Contains(script, "Ctrl+X: detach") {
		t.Errorf("expected status-right to show 'Ctrl+X: detach', script:\n%s", script)
	}
}

func TestBuildAttachScript_QuotingPreserved(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+q")
	// Title with a single quote must be escaped via the existing sq helper.
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "Doug's project", spec)

	// `'\''` is the POSIX-correct close-quote-escape-quote-reopen-quote
	// sequence the existing sq helper produces; we just check the dangerous
	// raw form does not appear (i.e. quoting was applied).
	if strings.Contains(script, "Doug'sproject") {
		t.Errorf("title quoting failed; script contains unescaped form:\n%s", script)
	}
	if !strings.Contains(script, `Doug'\''s project`) {
		t.Errorf("expected POSIX-escaped form `Doug'\\''s project` in script:\n%s", script)
	}
}

func TestBuildAttachScript_QuotesSessionWithSingleQuote(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+q")
	script := buildAttachScript("hive's-sess", "hive's-sess:0", "it's a test", spec)
	// Raw, unescaped form must NOT appear.
	if strings.Contains(script, "hive's-sess:0'") {
		t.Errorf("unescaped single quotes in target:\n%s", script)
	}
	if !strings.Contains(script, "tmux attach-session") {
		t.Errorf("script should contain attach command:\n%s", script)
	}
}

// TestBuildAttachScript_StatusBarShape mirrors the assertions from the old
// internal/tui TestBuildAttachScript which used to live next to the function
// before #41. The status-bar-style coverage moved here together with the
// implementation; the new bind-key save/restore assertions are in the
// dedicated tests above.
func TestBuildAttachScript_StatusBarShape(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+q")
	script := buildAttachScript(
		"hive-sessions",
		"hive-sessions:3",
		"● [claude] my-task · myproj ⎇ feat",
		spec,
	)

	wants := map[string]string{
		"sets status-position top":                 "status-position top",
		"attaches to the correct target":           "tmux attach-session -t 'hive-sessions:3'",
		"contains the title text":                  "● [claude] my-task · myproj ⎇ feat",
		"shows the detach hint with display key":   "Ctrl+Q: detach",
		"uses had_status flag for restore":         `had_status" = 1`,
		"restores via -u when option was unset":    "set-option -u",
		"enters alt screen before attach":          `\033[?1049h`,
		"trap leaves alt screen":                   `\033[?1049l`,
		"hides window list (status-format)":        "window-status-format ''",
		"hides window list (current-format)":       "window-status-current-format ''",
		"hides window list (separator)":            "window-status-separator ''",
		"saves window-status-format via had_*":     `had_window_status_format" = 1`,
		"injects literal #{pane_title} for tmux":   "#{pane_title}",
		"wraps pane_title in #{?pane_title,…,}":    "#{?pane_title,",
	}
	for desc, want := range wants {
		if !strings.Contains(script, want) {
			t.Errorf("script should %s — missing %q\n--- script ---\n%s", desc, want, script)
		}
	}
	if got := strings.Count(script, "#{pane_title}"); got != 1 {
		t.Errorf("expected exactly one #{pane_title} token, got %d", got)
	}
}
