package muxtmux

import (
	"fmt"
	"strings"

	"github.com/lucascaro/hive/internal/mux"
)

// statusBarOpts lists the tmux session options the attach script overrides
// to render the custom Hive status bar. Each option is saved before the
// override and restored after the user detaches.
var statusBarOpts = []string{
	"status",
	"status-position",
	"status-style",
	"status-left",
	"status-right",
	"status-left-length",
	"status-right-length",
	"window-status-format",
	"window-status-current-format",
	"window-status-separator",
}

// AttachScript returns a shell script that:
//
//  1. Installs a `bind-key -n <key> detach-client` so a single keystroke
//     returns the user to Hive. The binding is intentionally left in place
//     across attach/detach cycles — it is re-installed idempotently on
//     every attach, persists for the lifetime of the tmux server, and is
//     not cleaned up on detach. This keeps the trap body tiny and removes
//     a whole class of per-detach save/restore fragility. The trade-off
//     is that a user-defined `bind -n C-q ...` in `~/.tmux.conf` will
//     stay clobbered while (and after) hive runs; users who need to keep
//     their own binding should set `detach_key` to a different `ctrl+<letter>`.
//  2. Saves and overrides the tmux status-bar options to render the custom
//     Hive header (project, agent, live pane title, detach hint).
//  3. Runs `tmux attach-session -t <target>`.
//  4. On exit (including signals), restores the alternate screen and the
//     status-bar options via a shell `trap`.
//
// The script is intentionally a single shell pipeline so it composes with
// `tea.ExecProcess` (TUI attach), `sh -c` (headless `hive attach` CLI), and
// `tmux display-popup -E` (popup attach) without per-call special-casing.
//
// The session name is derived from target ("session:window"); callers do
// not need to pass it separately.
func (b *Backend) AttachScript(target, title string) string {
	return buildAttachScript(sessionFromTarget(target), target, title, b.spec)
}

// buildAttachScript is the package-level implementation used by AttachScript
// and by Backend.Attach / Backend.PopupAttach. Pulled out so it's directly
// testable without constructing a full Backend.
func buildAttachScript(tmuxSession, target, title string, spec mux.DetachKeySpec) string {
	sq := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	s := sq(tmuxSession)
	t := sq(target)
	displayKey := spec.Display // e.g. Ctrl+Q

	var lines []string

	// Enter the alternate screen first so any tmux output below stays out
	// of the user's scrollback. This is intentionally redundant with
	// altScreenExecCmd.Run() in internal/tui/views.go which writes the same
	// sequence to the terminal immediately after BubbleTea's ReleaseTerminal
	// and before spawning this subprocess. Keeping the write here is the
	// belt-and-suspenders guarantee that the subprocess always runs in
	// alt-screen even if it is invoked by a caller that bypasses
	// altScreenExecCmd (e.g. `hive attach` CLI, tmux display-popup).
	lines = append(lines, `printf '\033[?1049h\033[2J'`)

	// Install the detach key binding. Idempotent and intentionally left in
	// place after detach — see the AttachScript doc comment for the
	// rationale (no per-detach save/restore, persists for the lifetime of
	// the tmux server). The tmux key spec is shell-safe (`C-<letter>`).
	lines = append(lines,
		fmt.Sprintf("tmux bind-key -n %s detach-client", spec.Tmux),
	)

	// Store the session name in a shell variable so the trap body does not
	// need to embed single-quoted strings (which would break the trap's own
	// single-quoted delimiters for session names containing special chars).
	lines = append(lines, "_hive_s="+s)

	// trap unsets the status-bar option overrides and clears the screen on
	// exit. Hive owns the hive-* session, so there are no user-customized
	// values to preserve — `set-option -u` returns each option to its
	// server/global default from ~/.tmux.conf.
	//
	// EXIT covers normal exit; INT/TERM/HUP cover the case where
	// tea.ExecProcess (or the user's terminal) forwards a signal from a
	// hive shutdown so the status bar does not stay flipped to Hive's
	// theme. The detach key binding is NOT restored here — it stays
	// installed for the tmux server's lifetime.
	//
	// Order matters: the tmux cleanup runs FIRST (while still in alt-screen,
	// so the user never sees the primary buffer during the cleanup delay),
	// then the screen is cleared. We do NOT write \033[?1049l (exit
	// alt-screen) — that would expose the primary terminal buffer while the
	// tmux subprocess completes. BubbleTea's RestoreTerminal re-enters
	// alt-screen immediately after and redraws the TUI.
	//
	// All unsets are batched into a single tmux invocation via \; to
	// minimize process-spawn overhead on detach.
	var unsetParts []string
	for _, opt := range statusBarOpts {
		unsetParts = append(unsetParts, fmt.Sprintf(`set-option -u -t "$_hive_s" %s`, opt))
	}
	trapBody := fmt.Sprintf(`tmux %s 2>/dev/null; printf "\033[2J\033[H"`, strings.Join(unsetParts, ` \; `))
	lines = append(lines,
		"trap '"+trapBody+"' EXIT INT TERM HUP",
	)

	// Override status-bar options with the Hive look. All options are
	// batched into a single tmux invocation via \; chaining to minimize
	// process-spawn overhead (was 11 separate invocations).
	//
	// The "{?pane_title, · …,}" conditional makes the separator and live
	// title disappear cleanly when the pane has no title set, instead of
	// rendering a dangling " · " after the static session header.
	setOpts := []string{
		"set-option -t " + s + " status on",
		"set-option -t " + s + " status-position top",
		"set-option -t " + s + " status-style 'bg=#7C3AED,fg=#F9FAFB'",
		"set-option -t " + s + " status-left " + sq(" "+title+"#{?pane_title, · #{pane_title},} "),
		"set-option -t " + s + " status-left-length 200",
		"set-option -t " + s + " status-right " + sq(" "+displayKey+": detach "),
		"set-option -t " + s + " status-right-length 40",
		// Hide tmux's default window list — we only want our custom title and
		// the live #{pane_title} above.  Empty formats collapse the entries;
		// the empty separator is belt-and-suspenders for tmux 2.x.
		"set-option -t " + s + " window-status-format ''",
		"set-option -t " + s + " window-status-current-format ''",
		"set-option -t " + s + " window-status-separator ''",
	}
	lines = append(lines, "tmux "+strings.Join(setOpts, ` \; `))

	lines = append(lines, "tmux attach-session -t "+t)

	return strings.Join(lines, "\n")
}
