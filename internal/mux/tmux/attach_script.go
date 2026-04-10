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
	// of the user's scrollback.
	lines = append(lines, `printf '\033[?1049h\033[2J'`)

	// Initialize had_* flags to 0 so the trap below has well-defined values
	// if it fires before the save loop runs (e.g. tmux is killed mid-setup).
	// Without this, an early-crash trap would treat unset variables as "no
	// prior value" and stomp on user-customized status-bar options.
	for _, opt := range statusBarOpts {
		v := strings.ReplaceAll(opt, "-", "_")
		lines = append(lines, fmt.Sprintf("had_%s=0", v))
	}

	// Install the detach key binding. Idempotent and intentionally left in
	// place after detach — see the AttachScript doc comment for the
	// rationale (no per-detach save/restore, persists for the lifetime of
	// the tmux server). The tmux key spec is shell-safe (`C-<letter>`).
	lines = append(lines,
		fmt.Sprintf("tmux bind-key -n %s detach-client", spec.Tmux),
	)

	// trap restores the alternate screen and the status-bar options on
	// exit. EXIT covers normal exit; INT/TERM/HUP cover the case where
	// tea.ExecProcess (or the user's terminal) forwards a signal from a
	// hive shutdown so the status bar does not stay flipped to Hive's
	// theme on the user's tmux session. The detach key binding is NOT
	// restored here — it stays installed for the tmux server's lifetime.
	//
	// Every entry in restoreLines MUST be a complete one-line shell statement.
	// We join them with "; " before embedding in the trap body; a split
	// `if / then / else / fi` across multiple entries would produce `; then;`
	// — parseable as a string by `sh -n` but a runtime syntax error when the
	// trap actually fires (the inner if-block is only re-parsed at exit).
	var restoreLines []string
	restoreLines = append(restoreLines, `printf "\033[?1049l"`)
	for _, opt := range statusBarOpts {
		v := strings.ReplaceAll(opt, "-", "_")
		restoreLines = append(restoreLines,
			fmt.Sprintf(`if [ "$had_%s" = 1 ]; then tmux set-option -t %s %s "$old_%s"; else tmux set-option -u -t %s %s 2>/dev/null; fi`,
				v, s, opt, v, s, opt))
	}
	lines = append(lines,
		"trap '"+strings.Join(restoreLines, "; ")+"' EXIT INT TERM HUP",
	)

	// Save existing status-bar option values so the trap can restore them.
	for _, opt := range statusBarOpts {
		v := strings.ReplaceAll(opt, "-", "_")
		lines = append(lines,
			fmt.Sprintf("old_%s=$(tmux show-option -t %s -v %s 2>/dev/null) && had_%s=1 || had_%s=0",
				v, s, opt, v, v))
	}

	// Override status-bar options with the Hive look.
	lines = append(lines,
		"tmux set-option -t "+s+" status on",
		"tmux set-option -t "+s+" status-position top",
		"tmux set-option -t "+s+" status-style 'bg=#7C3AED,fg=#F9FAFB'",
		// The "{?pane_title, · …,}" conditional makes the separator and live
		// title disappear cleanly when the pane has no title set, instead of
		// rendering a dangling " · " after the static session header.
		"tmux set-option -t "+s+" status-left "+sq(" "+title+"#{?pane_title, · #{pane_title},} "),
		"tmux set-option -t "+s+" status-left-length 200",
		"tmux set-option -t "+s+" status-right "+sq(" "+displayKey+": detach "),
		"tmux set-option -t "+s+" status-right-length 40",
		// Hide tmux's default window list — we only want our custom title and
		// the live #{pane_title} above.  Empty formats collapse the entries;
		// the empty separator is belt-and-suspenders for tmux 2.x.
		"tmux set-option -t "+s+" window-status-format ''",
		"tmux set-option -t "+s+" window-status-current-format ''",
		"tmux set-option -t "+s+" window-status-separator ''",
	)

	lines = append(lines, "tmux attach-session -t "+t)

	return strings.Join(lines, "\n")
}
