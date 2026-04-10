package muxtmux

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/mux"
)

func TestBuildAttachScript_BindKeyInstall(t *testing.T) {
	spec, err := mux.ParseDetachKey("ctrl+q")
	if err != nil {
		t.Fatalf("ParseDetachKey: %v", err)
	}
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "demo · claude", spec)

	wantSubstrings := []string{
		// Install our binding (idempotent; persists for tmux server lifetime).
		`tmux bind-key -n C-q detach-client`,
		// Trap covers EXIT and signal-driven shutdowns so the alt screen and
		// status bar are restored even if hive is killed mid-attach.
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

	// The binding is intentionally not saved/restored on detach — see
	// AttachScript doc comment. Guard against re-introducing the per-detach
	// save/unbind logic, which would resurrect the trap-syntax bug class
	// and the racy attach/detach binding lifecycle.
	unwantedSubstrings := []string{
		`old_detach=`,            // no save of prior binding
		`tmux unbind-key -n C-q`, // no unbind in trap
		`list-keys -T root`,      // no save query
	}
	for _, unwanted := range unwantedSubstrings {
		if strings.Contains(script, unwanted) {
			t.Errorf("attach script must not contain %q (per-detach binding restore was removed)\n--- script ---\n%s", unwanted, script)
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

func TestBuildAttachScript_StatusBarShape(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+q")
	script := buildAttachScript(
		"hive-sessions",
		"hive-sessions:3",
		"● [claude] my-task · myproj ⎇ feat",
		spec,
	)

	wants := map[string]string{
		"sets status-position top":               "status-position top",
		"attaches to the correct target":         "tmux attach-session -t 'hive-sessions:3'",
		"contains the title text":                "● [claude] my-task · myproj ⎇ feat",
		"shows the detach hint with display key": "Ctrl+Q: detach",
		"restores via -u in trap":                "set-option -u",
		"enters alt screen before attach":        `\033[?1049h`,
		"trap leaves alt screen":                 `\033[?1049l`,
		"hides window list (status-format)":      "window-status-format ''",
		"hides window list (current-format)":     "window-status-current-format ''",
		"hides window list (separator)":          "window-status-separator ''",
		"injects literal #{pane_title} for tmux": "#{pane_title}",
		"wraps pane_title in #{?pane_title,…,}":  "#{?pane_title,",
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

func TestBuildAttachScript_NoSavePhase(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+q")
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "title", spec)

	// The save phase (show-option, had_*, old_*) was removed — Hive owns the
	// session so there are no user values to preserve.
	unwanted := []string{
		"show-option",
		"had_",
		"old_",
	}
	for _, u := range unwanted {
		if strings.Contains(script, u) {
			t.Errorf("script must not contain %q (save phase was removed)\n--- script ---\n%s", u, script)
		}
	}
}

func TestBuildAttachScript_BatchedCommands(t *testing.T) {
	spec, _ := mux.ParseDetachKey("ctrl+q")
	script := buildAttachScript("hive-sessions", "hive-sessions:0", "title", spec)

	// Count lines starting with "tmux " — should be exactly 3:
	// 1. bind-key
	// 2. set-option chain (override)
	// 3. attach-session
	// The trap body contains 1 more tmux invocation but it's inline.
	tmuxCount := 0
	for _, line := range strings.Split(script, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "tmux ") {
			tmuxCount++
		}
	}
	// 3 top-level tmux lines (bind-key, set-option chain, attach-session)
	// The trap body contains 1 more tmux invocation but it's inline, not a separate line.
	if tmuxCount != 3 {
		t.Errorf("expected 3 top-level tmux invocations, got %d\n--- script ---\n%s", tmuxCount, script)
	}

	// The override set-option must use \; chaining.
	setLine := ""
	for _, line := range strings.Split(script, "\n") {
		if strings.Contains(line, "set-option") && strings.Contains(line, `\;`) && !strings.Contains(line, "trap") {
			setLine = line
			break
		}
	}
	if setLine == "" {
		t.Fatalf("expected a chained set-option line with \\;\n--- script ---\n%s", script)
	}

	// All 10 status bar options must appear in the chained set-option line.
	for _, opt := range statusBarOpts {
		if !strings.Contains(setLine, opt) {
			t.Errorf("chained set-option missing option %q\n--- line ---\n%s", opt, setLine)
		}
	}

	// The trap body must also use \; chaining for unsets.
	trapIdx := strings.Index(script, "trap '")
	if trapIdx < 0 {
		t.Fatalf("trap not found in script")
	}
	trapLine := script[trapIdx:]
	if endIdx := strings.Index(trapLine, "' EXIT"); endIdx > 0 {
		trapLine = trapLine[:endIdx]
	}
	for _, opt := range statusBarOpts {
		if !strings.Contains(trapLine, "set-option -u") || !strings.Contains(trapLine, opt) {
			t.Errorf("trap missing unset for option %q\n--- trap ---\n%s", opt, trapLine)
		}
	}
}
