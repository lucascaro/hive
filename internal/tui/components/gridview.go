package components

import (
	"math"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// GridKeys holds the configurable bindings GridView consults during input
// handling. The parent (tui.Model) sets this once from the active KeyMap
// (see tui.New) before the Bubble Tea program starts dispatching, so rebinding
// any of these actions in config takes effect inside the grid.
//
// Distinct from tui.GridKeyMap, which is the help-overlay description of the
// grid hint line. GridKeys is the actual input-routing table used by Update;
// when the zero value is left in place every key.Matches lookup is a no-op
// (charm's empty key.Binding{} matches nothing).
type GridKeys struct {
	Detach      key.Binding
	InputMode   key.Binding
	Attach      key.Binding
	CursorUp    key.Binding
	CursorDown  key.Binding
	CursorLeft  key.Binding
	CursorRight key.Binding
}

// GridSessionSelectedMsg is sent when the user selects a session in the grid.
type GridSessionSelectedMsg struct {
	TmuxSession string
	TmuxWindow  int
}

// GridPreviewsUpdatedMsg carries fresh capture-pane content for all sessions.
// Fast=true indicates this is from the input-mode focused-session poll loop;
// the handler uses this to reschedule the correct loop.
// Partial=true indicates the batch excluded some sessions (e.g. the focused
// session during input mode); the handler uses MergeContents to avoid blanking
// the excluded sessions' content.
type GridPreviewsUpdatedMsg struct {
	Contents   map[string]string
	Fast       bool
	Partial    bool
	Generation uint64
}

// PollGridPreviews returns a tea.Cmd that captures pane content for all sessions.
// gen is the grid-poll generation; the handler discards stale messages so old
// chains created by rapid mode toggles die off without compounding the rate.
// When partial is true, the result carries Partial=true so the handler uses
// MergeContents instead of SetContents (avoids blanking excluded sessions).
func PollGridPreviews(sessions []*state.Session, interval time.Duration, gen uint64, partial bool) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		targets := make(map[string]int, len(sessions))
		targetToID := make(map[string]string, len(sessions))
		for _, sess := range sessions {
			if sess.TmuxSession == "" {
				continue
			}
			target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
			targets[target] = 100
			targetToID[target] = sess.ID
		}
		captured, err := mux.BatchCapturePane(targets, true)
		contents := make(map[string]string, len(sessions))
		if err == nil {
			for target, content := range captured {
				if sid, ok := targetToID[target]; ok {
					contents[sid] = content
				}
			}
		}
		return GridPreviewsUpdatedMsg{Contents: contents, Generation: gen, Partial: partial}
	})
}

// PollFocusedGridPreview returns a tea.Cmd that captures pane content for a
// single session at the given interval. Used for the fast poll in input mode.
// The returned message has Fast=true so the handler reschedules this loop.
func PollFocusedGridPreview(sess *state.Session, interval time.Duration) tea.Cmd {
	if sess == nil || sess.TmuxSession == "" {
		return nil
	}
	target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
	sessID := sess.ID
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		contents := make(map[string]string, 1)
		if content, err := mux.CapturePane(target, 100); err == nil {
			contents[sessID] = content
		}
		return GridPreviewsUpdatedMsg{Contents: contents, Fast: true}
	})
}

// GridView renders all sessions as a tiled grid with live previews.
type GridView struct {
	Active       bool
	Cursor       int
	Width        int
	Height       int
	Mode         state.GridRestoreMode
	sessions      []*state.Session
	contents      map[string]string // sanitized content for rendering
	rawContents   map[string]string // last raw capture per session for change detection
	projectNames  map[string]string // projectID → display name
	projectColors map[string]string // projectID → hex color
	sessionColors map[string]string // sessionID → hex color
	paneTitles    map[string]string // target ("tmuxSession:windowIdx") → live pane title
	bellPending   map[string]bool   // sessionID → true when an unacknowledged bell has fired
	bellBlinkOn   bool              // toggled by the bell-blink ticker; true = show ♪, false = show status dot
	atExtended    bool              // true when cursor is visually at the extended (lower) portion of a cell
	inputMode         bool // true when keystrokes are forwarded to the focused session
	InputEnabled      bool // when false, InputMode key is a no-op (set from cfg.DisableGridInput)
	QuickReplyEnabled bool // when true, 1-9 keys send digit to the focused session
	// Keys holds the configurable bindings consulted by Update. The parent
	// (tui.Model) sets this once during construction; leaving the zero value
	// in place disables every navigation/input key inside the grid.
	Keys GridKeys
}

// Show activates the grid with the given sessions.
func (gv *GridView) Show(sessions []*state.Session, mode state.GridRestoreMode) {
	gv.Active = true
	gv.Mode = mode
	gv.sessions = sessions
	gv.atExtended = false // reset when session count changes (layout may change)
	if gv.Cursor >= len(sessions) {
		gv.Cursor = 0
	}
	if gv.contents == nil {
		gv.contents = make(map[string]string)
	}
	if gv.rawContents == nil {
		gv.rawContents = make(map[string]string)
	}
}

// Hide deactivates the grid.
// atExtended is intentionally not reset here — Show() clears it on the next
// grid open, and Update() is gated on Active so the stale value is never read.
// inputMode is cleared so a re-opened grid always starts in nav mode.
func (gv *GridView) Hide() {
	gv.Active = false
	gv.inputMode = false
}

// SetContents replaces the captured preview content map with raw content,
// sanitizing only sessions whose content has changed since the last poll.
func (gv *GridView) SetContents(rawContents map[string]string) {
	if gv.rawContents == nil {
		gv.rawContents = make(map[string]string, len(rawContents))
	}
	if gv.contents == nil {
		gv.contents = make(map[string]string, len(rawContents))
	}
	// Remove sessions no longer present in the new batch.
	for k := range gv.rawContents {
		if _, ok := rawContents[k]; !ok {
			delete(gv.rawContents, k)
			delete(gv.contents, k)
		}
	}
	for k, raw := range rawContents {
		if prev, ok := gv.rawContents[k]; ok && prev == raw {
			continue // unchanged — skip sanitization
		}
		gv.rawContents[k] = raw
		gv.contents[k] = sanitizePreviewContent(raw)
	}
}

// ContentFor returns the cached preview content for a session (used in tests).
func (gv *GridView) ContentFor(sessionID string) string {
	if gv.contents == nil {
		return ""
	}
	return gv.contents[sessionID]
}

// MergeContents updates only the keys present in rawContents, leaving other
// sessions' content untouched. Sanitizes only when raw content has changed.
// Used by the focused-session fast poll so it doesn't blank out non-focused cells.
func (gv *GridView) MergeContents(rawContents map[string]string) {
	if gv.rawContents == nil {
		gv.rawContents = make(map[string]string, len(rawContents))
	}
	if gv.contents == nil {
		gv.contents = make(map[string]string, len(rawContents))
	}
	for k, raw := range rawContents {
		if prev, ok := gv.rawContents[k]; ok && prev == raw {
			continue // unchanged — skip sanitization
		}
		gv.rawContents[k] = raw
		gv.contents[k] = sanitizePreviewContent(raw)
	}
}

// SetProjectNames provides a projectID→name lookup used in cell headers.
func (gv *GridView) SetProjectNames(names map[string]string) {
	gv.projectNames = names
}

// SetProjectColors provides a projectID→hex color lookup used in cell headers.
func (gv *GridView) SetProjectColors(colors map[string]string) {
	gv.projectColors = colors
}

// SetSessionColors provides a sessionID→hex color lookup used for cell borders.
func (gv *GridView) SetSessionColors(colors map[string]string) {
	gv.sessionColors = colors
}

// SetPaneTitles provides a target→live pane title lookup used to render an
// optional subtitle row inside each cell.  Keyed by tmux target string
// (mux.Target output) to match how titles arrive from the status watcher.
func (gv *GridView) SetPaneTitles(titles map[string]string) {
	gv.paneTitles = titles
}

// SetBellPending updates the set of sessions with an unacknowledged bell.
// Keyed by sessionID; cells with a pending bell display a ♪ badge.
func (gv *GridView) SetBellPending(bells map[string]bool) {
	gv.bellPending = bells
}

// SetBellBlink updates the animated on/off state for the bell badge.
func (gv *GridView) SetBellBlink(on bool) {
	gv.bellBlinkOn = on
}

// BellPendingForTest reports whether the given sessionID has a pending bell.
// For use in tests only — not part of the production API.
func (gv *GridView) BellPendingForTest(sessionID string) bool {
	return gv.bellPending[sessionID]
}

// AtExtendedForTest reports whether the cursor is currently at the extended
// (visual bottom) portion of a cell.
// For use in tests only — not part of the production API.
func (gv *GridView) AtExtendedForTest() bool {
	return gv.atExtended
}

// InputMode reports whether the grid is in keystroke-forwarding mode.
func (gv GridView) InputMode() bool {
	return gv.inputMode
}

// ExitInputMode clears input mode without any other side-effect.
// Used externally when the grid is hidden while in input mode.
func (gv *GridView) ExitInputMode() {
	gv.inputMode = false
}

// Selected returns the currently focused session, or nil.
func (gv *GridView) Selected() *state.Session {
	if !gv.Active || gv.Cursor < 0 || gv.Cursor >= len(gv.sessions) {
		return nil
	}
	return gv.sessions[gv.Cursor]
}

// SyncState refreshes the grid's sessions, metadata maps, and cursor position
// in a single call. Use this after any operation that changes session order,
// project/session colors, or the active session set.
func (gv *GridView) SyncState(sessions []*state.Session, mode state.GridRestoreMode, projectNames, projectColors, sessionColors map[string]string, cursorSessionID string) {
	gv.Show(sessions, mode)
	gv.projectNames = projectNames
	gv.projectColors = projectColors
	gv.sessionColors = sessionColors
	gv.SyncCursor(cursorSessionID)
}

// SyncCursor moves the cursor to the session matching sessionID.
func (gv *GridView) SyncCursor(sessionID string) {
	if sessionID == "" {
		return
	}
	for i, sess := range gv.sessions {
		if sess.ID == sessionID {
			gv.Cursor = i
			gv.atExtended = false
			return
		}
	}
}

// Update handles key events for the grid view.
// Returns (cmd, consumed). consumed=true means this component handled the key.
func (gv *GridView) Update(msg tea.KeyMsg) (tea.Cmd, bool) {
	if !gv.Active {
		return nil, false
	}

	// Input mode: forward all keystrokes to the focused session.
	// The configured Detach key exits input mode; everything else (including
	// Esc) is forwarded.
	if gv.inputMode {
		if key.Matches(msg, gv.Keys.Detach) {
			gv.inputMode = false
			return nil, true
		}
		sess := gv.Selected()
		if sess != nil && sess.TmuxSession != "" {
			target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
			keys := keyToBytes(msg)
			if keys != "" {
				return func() tea.Msg {
					mux.SendKeys(target, keys) //nolint:errcheck // best-effort
					return nil
				}, true
			}
		}
		return nil, true
	}

	// Quick-reply: in navigation mode, pressing a digit 1-9 sends that digit
	// + Enter to the focused session. Works in all session states so the user
	// can quickly answer numbered prompts without attaching or entering input mode.
	if gv.QuickReplyEnabled {
		if r := msg.String(); len(r) == 1 && r[0] >= '1' && r[0] <= '9' {
			if sess := gv.Selected(); sess != nil && sess.TmuxSession != "" {
				target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
				digit := r
				return func() tea.Msg {
					mux.SendKeys(target, digit) //nolint:errcheck // best-effort
					return nil
				}, true
			}
		}
	}

	n := len(gv.sessions)
	cols := gridColumns(gv.Width, gv.Height, n)

	rows := (n + cols - 1) / cols

	switch {
	case key.Matches(msg, gv.Keys.InputMode):
		if gv.InputEnabled {
			if sess := gv.Selected(); sess != nil && sess.TmuxSession != "" {
				gv.inputMode = true
			}
		}
		return nil, true
	// Esc remains a hard-coded literal as a universal "close overlay" gesture,
	// matching the dialog/overlay convention elsewhere in the TUI. Will move
	// into GridKeys (alongside a configurable Cancel) in chunk 3 of #112.
	case msg.String() == "esc":
		gv.Hide()
		return nil, true
	case key.Matches(msg, gv.Keys.Attach):
		if sess := gv.Selected(); sess != nil {
			s := sess
			return func() tea.Msg {
				return GridSessionSelectedMsg{TmuxSession: s.TmuxSession, TmuxWindow: s.TmuxWindow}
			}, true
		}
		return nil, true
	case key.Matches(msg, gv.Keys.CursorLeft):
		if gv.atExtended {
			// Cursor is at the visual bottom (extended row) of its session.
			// Navigate left within that virtual last row.
			col := gv.Cursor % cols
			if col > 0 {
				virtualTarget := (rows-1)*cols + (col - 1)
				if virtualTarget < n {
					// Real session to the left in the last row.
					gv.Cursor = virtualTarget
					gv.atExtended = false
				} else if n%cols != 0 && (col-1) >= n%cols {
					// Another extended slot to the left; navigate to its owner.
					ownerIdx := (rows-2)*cols + (col - 1)
					if ownerIdx >= 0 && ownerIdx < n {
						gv.Cursor = ownerIdx
						// atExtended stays true
					}
				}
			}
		} else {
			// Both branches decrement by one: within-row move and prev-row wrap
			// are identical operations (cursor layout is a flat index).
			if gv.Cursor > 0 {
				gv.Cursor--
			}
		}
	case key.Matches(msg, gv.Keys.CursorRight):
		row := gv.Cursor / cols
		if gv.atExtended {
			row = rows - 1 // treat cursor as being in the visual last row
		}
		col := gv.Cursor % cols
		nextCol := col + 1
		nextIdx := row*cols + nextCol
		switch {
		case nextCol < cols && nextIdx < n:
			// Normal move right within the same row.
			gv.Cursor = nextIdx
			gv.atExtended = false
		case nextCol < cols && nextIdx >= n && n%cols != 0 && nextCol >= n%cols && row == rows-1 && rows > 1:
			// Next slot is an extended cell — navigate to its owning session.
			ownerIdx := (rows-2)*cols + nextCol
			if ownerIdx >= 0 && ownerIdx < n {
				gv.Cursor = ownerIdx
				gv.atExtended = true
			}
		case row < rows-1:
			// End of row: wrap to first cell of the next row.
			nextRowStart := (row + 1) * cols
			if nextRowStart < n {
				gv.Cursor = nextRowStart
				gv.atExtended = false
			}
		}
	case key.Matches(msg, gv.Keys.CursorUp):
		gv.atExtended = false
		if gv.Cursor >= cols {
			gv.Cursor -= cols
		}
	case key.Matches(msg, gv.Keys.CursorDown):
		gv.atExtended = false
		if gv.Cursor+cols < n {
			gv.Cursor += cols
		}
	}
	return nil, true // consume all keys while grid is active
}

// View renders the full-screen grid.
// hints is a pre-computed one-line hint string rendered by the caller.
func (gv *GridView) View(hints string) string {
	if !gv.Active {
		return ""
	}
	n := len(gv.sessions)
	if n == 0 {
		return lipgloss.NewStyle().
			Width(gv.Width).
			Height(gv.Height).
			Align(lipgloss.Center, lipgloss.Center).
			Foreground(styles.ColorMuted).
			Render("No active sessions\n\nCreate a session with 't'")
	}

	cols := gridColumns(gv.Width, gv.Height, n)
	rows := (n + cols - 1) / cols
	hintH := 2
	cellW := gv.Width / cols
	totalH := gv.Height - hintH
	cellH := totalH / rows
	if cellH < 5 {
		cellH = 5
	}
	// Remaining height after all full-height rows: used to extend the last
	// content cell in each column so nothing is wasted.
	lastCellH := totalH - (rows-1)*cellH
	if lastCellH < 5 {
		lastCellH = 5
	}

	// Render column-by-column so that columns whose last row is empty can
	// extend the cell above into the unused space.
	//   n%cols == 0  → every column has content in the last row (no empties)
	//   c >= n%cols  → this column has no content in the last row
	var colViews []string
	for c := 0; c < cols; c++ {
		emptyInLastRow := n%cols != 0 && c >= n%cols
		var cellViews []string
		for r := 0; r < rows; r++ {
			idx := r*cols + c
			if idx >= n {
				// Empty slot — skip; the cell above is already extended.
				continue
			}
			var h int
			switch {
			case emptyInLastRow && r == rows-2:
				// Last content cell in this column: absorb the empty row below.
				h = cellH + lastCellH
			case r == rows-1:
				// Last row of a fully-occupied column: use remaining height.
				h = lastCellH
			default:
				h = cellH
			}
			cellViews = append(cellViews, gv.renderCell(gv.sessions[idx], cellW, h, idx == gv.Cursor, gv.inputMode && idx != gv.Cursor))
		}
		if len(cellViews) > 0 {
			colViews = append(colViews, lipgloss.JoinVertical(lipgloss.Left, cellViews...))
		}
	}

	grid := lipgloss.JoinHorizontal(lipgloss.Top, colViews...)
	// Truncate hint lines to gv.Width before joining with the grid.
	// Without this, JoinVertical computes maxWidth = max(grid_width, hint_width).
	// When hint_width (93 display chars) exceeds gv.Width (narrow terminals,
	// 60–92 cols), every grid row is padded to hint_width > TermWidth, causing
	// physical terminal line-wrap even though logical line count is correct.
	hintLine1 := ansi.Truncate(styles.MutedStyle.Render(styles.StatusLegend()), gv.Width, "")
	hintLine2 := ansi.Truncate(hints, gv.Width, "")
	hint := lipgloss.JoinVertical(lipgloss.Left, hintLine1, hintLine2)
	out := lipgloss.JoinVertical(lipgloss.Left, grid, hint)
	// Clamp to exactly gv.Height lines: integer-division of cellH can leave
	// the grid 1 line short or long. Hard-clamping here is the safety net.
	outLines := strings.Count(out, "\n") + 1
	if outLines < gv.Height {
		out += strings.Repeat("\n"+strings.Repeat(" ", gv.Width), gv.Height-outLines)
	} else if outLines > gv.Height {
		parts := strings.SplitN(out, "\n", gv.Height+1)
		out = strings.Join(parts[:gv.Height], "\n")
	}
	return out
}

func (gv *GridView) renderCell(sess *state.Session, w, h int, selected, dimmed bool) string {
	borderColor := styles.ColorBorder
	if selected {
		borderColor = styles.ColorAccent
	} else if dimmed {
		borderColor = styles.ColorDimmedBorder
	}
	borderStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor)

	// border=2cols → inner width = w-2
	// border top+bottom=2rows + header row → inner content height = h-3
	innerW := w - 2
	innerH := h - 3
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 1 {
		innerH = 1
	}

	// Header line — single line: status dot, agent badge, session title,
	// project name (muted, inline), and optional worktree badge (⎇).
	// All parts are rendered with the project background to avoid ANSI resets
	// breaking the background color.
	projColor := styles.ProjectColorOrDefault(gv.projectColors[sess.ProjectID])
	if dimmed {
		projColor = styles.DimHex(projColor)
	}
	bg := lipgloss.Color(projColor)
	fg := styles.ContrastForeground(projColor)

	// Keep the status dot and agent badge on a dark background so their
	// bright foreground colors remain legible regardless of project color.
	darkBg := lipgloss.Color(styles.ColorBg)
	// Bell badge replaces the status dot when a bell is pending. The badge is
	// toggled on/off by bellBlinkOn (driven by a tea.Tick in the Model) to
	// create a software blink effect independent of terminal ANSI blink.
	var dotOrBell string
	if gv.bellPending[sess.ID] && gv.bellBlinkOn {
		dotOrBell = styles.BellBadgeOnBg(darkBg)
	} else {
		dotOrBell = styles.StatusDotOnBg(string(sess.Status), darkBg)
	}
	badge := styles.AgentBadgeOnBg(string(sess.AgentType), darkBg)
	darkSp := lipgloss.NewStyle().Background(darkBg).Render(" ")
	bgSp := lipgloss.NewStyle().Background(bg).Foreground(fg).Render(" ")
	prefixStr := dotOrBell + darkSp + badge + bgSp
	prefixW := ansi.StringWidth(prefixStr)

	// Build the optional suffix (project + worktree) as plain text for width calc.
	projName := gv.projectNames[sess.ProjectID]
	suffixPlain := ""
	if projName != "" {
		suffixPlain = " · " + projName
	}
	if sess.WorktreePath != "" {
		if sess.WorktreeBranch != "" && sess.WorktreeBranch != sess.Title {
			suffixPlain += " ⎇ " + sess.WorktreeBranch
		} else {
			suffixPlain += " ⎇"
		}
	}

	// Give the title whatever space remains; drop suffix if it won't fit.
	const minTitleW = 4
	availW := innerW - prefixW
	suffixW := ansi.StringWidth(suffixPlain)
	var titleStr, suffix string
	if suffixW > 0 && availW-suffixW >= minTitleW {
		titleStr = ansi.Truncate(sess.Title, availW-suffixW, "…")
		suffix = suffixPlain
	} else {
		titleStr = ansi.Truncate(sess.Title, availW, "…")
	}

	bgStyle := lipgloss.NewStyle().Background(bg).Foreground(fg)
	// Build the text portion (title + suffix + padding) that follows the prefix.
	// Pad to fill availW so the cell background extends to the right edge.
	textPortion := titleStr + suffix
	actualTextW := ansi.StringWidth(textPortion)
	if pad := availW - actualTextW; pad > 0 {
		textPortion += strings.Repeat(" ", pad)
	}
	// Render with gradient if the session has its own color; flat otherwise.
	// Dim sessColor in sync with projColor so gradient endpoints match.
	sessColor := gv.sessionColors[sess.ID]
	if dimmed && sessColor != "" {
		sessColor = styles.DimHex(sessColor)
	}
	var headerContent string
	if sessColor != "" && sessColor != projColor {
		headerContent = prefixStr + styles.GradientBg(textPortion, projColor, sessColor, selected)
	} else {
		titlePart := bgStyle.Bold(selected).Render(titleStr)
		suffixPart := bgStyle.Render(suffix)
		flat := titlePart + suffixPart
		flatW := ansi.StringWidth(prefixStr + flat)
		if pad := innerW - flatW; pad > 0 {
			flat += bgStyle.Render(strings.Repeat(" ", pad))
		}
		headerContent = prefixStr + flat
	}

	// When input mode is active on this cell, overlay a badge at the right edge.
	if gv.inputMode && selected {
		badge := " INPUT · C-Q"
		badgeStyle := lipgloss.NewStyle().
			Background(styles.ColorAccent).
			Foreground(lipgloss.Color("#000000")).
			Bold(true)
		badgeStr := badgeStyle.Render(badge)
		badgeW := ansi.StringWidth(badge)
		// Replace the last badgeW chars of the header with the badge so the
		// total width stays constant.
		headerW := ansi.StringWidth(headerContent)
		if headerW > badgeW {
			headerContent = ansi.Truncate(headerContent, headerW-badgeW, "") + badgeStr
		}
	}

	headerLine := headerContent

	// Optional pane-title subtitle: only render when the cell is tall enough
	// to spare a row without crushing the content preview, and when the agent
	// has actually set a title via OSC 0/2.  Pre-strip control characters —
	// pane titles are untrusted OSC payload and lipgloss does not sanitize.
	var subtitleLine string
	showSubtitle := false
	if h >= 8 {
		if t := sanitizePaneTitle(gv.paneTitles[mux.Target(sess.TmuxSession, sess.TmuxWindow)]); t != "" {
			trunc := ansi.Truncate(t, innerW, "…")
			subtitleStyle := lipgloss.NewStyle().
				Foreground(styles.ColorMuted).
				Italic(true).
				Width(innerW).MaxWidth(innerW)
			if selected {
				subtitleStyle = subtitleStyle.Background(styles.ColorGridSelected)
			}
			subtitleLine = subtitleStyle.Render(trunc)
			showSubtitle = true
			innerH-- // give the row back from the content area
			if innerH < 1 {
				innerH = 1
			}
		}
	}

	// Content preview — show the last innerH lines so the most recent output
	// is visible. Pre-truncate each line to innerW before passing to lipgloss:
	// without this, Width(innerW) word-wraps a 245-char tmux line into several
	// short lines and MaxHeight then discards the most-recent content, keeping
	// only the oldest wrapped fragment.  Pre-truncation also prevents
	// cellbuf.Wrap from unexpectedly expanding line count in edge cases.
	contentStyle := lipgloss.NewStyle().
		Width(innerW).Height(innerH).
		MaxWidth(innerW).MaxHeight(innerH)
	if selected {
		contentStyle = contentStyle.Background(styles.ColorGridSelected)
	} else if dimmed {
		contentStyle = contentStyle.Background(styles.ColorBg).Foreground(styles.ColorMuted)
	}
	var contentStr string
	if content := gv.contents[sess.ID]; content != "" {
		rawLines := strings.Split(lastNContentLines(content, innerH), "\n")
		for i, l := range rawLines {
			rawLines[i] = ansi.Truncate(l, innerW, "")
		}
		joined := strings.Join(rawLines, "\n")
		// Re-apply the selected background after ANSI SGR resets in the
		// captured content so the tint persists through reset sequences
		// emitted by the terminal session. Handles both \033[0m and the
		// shorthand \033[m variant.
		if selected {
			joined = strings.ReplaceAll(joined, "\033[0m", "\033[0m"+styles.GridSelectedBgEsc)
			joined = strings.ReplaceAll(joined, "\033[m", "\033[m"+styles.GridSelectedBgEsc)
		}
		contentStr = contentStyle.Render(joined)
	} else {
		contentStr = contentStyle.
			Foreground(styles.ColorMuted).
			Render("…")
	}

	parts := []string{headerLine}
	if showSubtitle {
		parts = append(parts, subtitleLine)
	}
	parts = append(parts, contentStr)
	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
	// MaxHeight(h) is a safety net: alignTextVertical (Height) only pads — it
	// never truncates.  If inner somehow exceeds h-2 lines, the bordered output
	// would be h+k lines.  MaxHeight(h) caps the final bordered cell at h lines.
	return borderStyle.Width(w - 2).Height(h - 2).MaxHeight(h).Render(inner)
}

// paneTitleSanitizeRe matches anything we want to strip from a raw pane title
// before rendering it inside a grid cell: ANSI CSI sequences (full ECMA-48
// shape: parameter bytes 0x30-0x3F, intermediate bytes 0x20-0x2F, final byte
// 0x40-0x7E — covers private-mode sequences like \x1b[?25l), ANSI OSC
// sequences (BEL- or ST-terminated), and any C0/DEL control characters.
// Pane titles come from agent OSC 0/2 escapes — untrusted input that lipgloss
// does not sanitize.
var paneTitleSanitizeRe = regexp.MustCompile(`\x1b\[[0-?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|[\x00-\x1f\x7f]`)

// sanitizePaneTitle strips ANSI escapes and control characters from a raw pane
// title and trims surrounding whitespace.  Returns "" if nothing useful remains.
func sanitizePaneTitle(s string) string {
	if s == "" {
		return ""
	}
	return strings.TrimSpace(paneTitleSanitizeRe.ReplaceAllString(s, ""))
}

// lastNLines returns the last n lines of s (split on '\n').
// If s has fewer than n lines, the full string is returned unchanged.
func lastNLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return s
	}
	return strings.Join(lines[len(lines)-n:], "\n")
}

// lastNContentLines returns the last n lines of s before any trailing blank
// rows.  capture-pane pads the terminal height with empty rows below the
// cursor; taking the raw last-n lines would show those blank rows instead of
// real content.  This mirrors the sidebar's lastNonBlankIdx approach.
func lastNContentLines(s string, n int) string {
	if n <= 0 {
		return ""
	}
	lines := strings.Split(s, "\n")
	// Find the last line with visible text.
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end == 0 {
		return ""
	}
	start := end - n
	if start < 0 {
		start = 0
	}
	return strings.Join(lines[start:end], "\n")
}

// MoveUp moves the grid cursor up by one row.
func (gv *GridView) MoveUp() {
	cols := gridColumns(gv.Width, gv.Height, len(gv.sessions))
	gv.atExtended = false
	if gv.Cursor >= cols {
		gv.Cursor -= cols
	}
}

// MoveDown moves the grid cursor down by one row.
func (gv *GridView) MoveDown() {
	n := len(gv.sessions)
	cols := gridColumns(gv.Width, gv.Height, n)
	gv.atExtended = false
	if gv.Cursor+cols < n {
		gv.Cursor += cols
	}
}

// CellAt maps a mouse click at terminal position (x, y) to a session index.
// Returns (idx, true) if the click lands on a valid session cell, or (-1, false)
// if it hits the hint bar, an empty padding cell, or is otherwise out of range.
func (gv *GridView) CellAt(x, y int) (idx int, ok bool) {
	n := len(gv.sessions)
	if n == 0 {
		return -1, false
	}
	cols := gridColumns(gv.Width, gv.Height, n)
	rows := (n + cols - 1) / cols
	const hintH = 2
	cellW := gv.Width / cols
	totalH := gv.Height - hintH
	cellH := totalH / rows
	if cellH < 5 {
		cellH = 5
	}
	lastCellH := totalH - (rows-1)*cellH
	if lastCellH < 5 {
		lastCellH = 5
	}
	// Clicks in the hint bar at the bottom are ignored.
	if y >= gv.Height-hintH {
		return -1, false
	}
	col := x / cellW
	if col >= cols {
		return -1, false
	}
	// Determine the row accounting for variable last-row height.
	// Columns with no content in the last row have their last real cell
	// extended by lastCellH, so clicks in that extension belong to rows-2.
	emptyInLastRow := n%cols != 0 && col >= n%cols
	var row int
	if emptyInLastRow {
		// This column only has rows-1 cells; the last one is extended.
		extendedCellH := cellH + lastCellH
		if y < (rows-1)*cellH {
			row = y / cellH
		} else if y < (rows-1)*cellH+extendedCellH {
			row = rows - 2
		} else {
			return -1, false
		}
	} else {
		if y < (rows-1)*cellH {
			row = y / cellH
		} else {
			row = rows - 1
		}
	}
	if row >= rows {
		return -1, false
	}
	i := row*cols + col
	if i >= n {
		return -1, false
	}
	return i, true
}

// keyToBytes converts a BubbleTea key message to the raw bytes to forward to a
// session in input mode. Returns "" for keys that have no sensible byte
// representation (e.g. unknown function keys).
func keyToBytes(msg tea.KeyMsg) string {
	switch msg.Type {
	case tea.KeyRunes:
		return string(msg.Runes)
	case tea.KeyEnter:
		return "\r"
	case tea.KeyBackspace:
		return "\x7f"
	case tea.KeyDelete:
		return "\x1b[3~"
	case tea.KeyTab:
		return "\t"
	case tea.KeySpace:
		return " "
	case tea.KeyUp:
		return "\033[A"
	case tea.KeyDown:
		return "\033[B"
	case tea.KeyRight:
		return "\033[C"
	case tea.KeyLeft:
		return "\033[D"
	case tea.KeyEscape:
		return "\033"
	// Common Ctrl+letter keys.
	case tea.KeyCtrlA:
		return "\x01"
	case tea.KeyCtrlB:
		return "\x02"
	case tea.KeyCtrlC:
		return "\x03"
	case tea.KeyCtrlD:
		return "\x04"
	case tea.KeyCtrlE:
		return "\x05"
	case tea.KeyCtrlF:
		return "\x06"
	case tea.KeyCtrlG:
		return "\x07"
	case tea.KeyCtrlH:
		return "\x08"
	case tea.KeyCtrlJ:
		return "\x0a"
	case tea.KeyCtrlK:
		return "\x0b"
	case tea.KeyCtrlL:
		return "\x0c"
	case tea.KeyCtrlN:
		return "\x0e"
	case tea.KeyCtrlO:
		return "\x0f"
	case tea.KeyCtrlP:
		return "\x10"
	case tea.KeyCtrlR:
		return "\x12"
	case tea.KeyCtrlS:
		return "\x13"
	case tea.KeyCtrlT:
		return "\x14"
	case tea.KeyCtrlU:
		return "\x15"
	case tea.KeyCtrlV:
		return "\x16"
	case tea.KeyCtrlW:
		return "\x17"
	case tea.KeyCtrlX:
		return "\x18"
	case tea.KeyCtrlY:
		return "\x19"
	case tea.KeyCtrlZ:
		return "\x1a"
	default:
		return ""
	}
}

// gridColumns computes the number of columns that best tiles n sessions inside
// a w×h terminal, maximising real-estate use.
//
// Scoring (lower is better, evaluated for each candidate column count):
//
//		score = waste×5  +  |cols−rows|×2  +  ratioDiff
//
//	  • waste      — wasted cells (cols×rows − n); strongly penalised.
//	  • |cols−rows| — prefer grids whose shape is close to square
//	                  (e.g. 2×2 beats 4×1 for n=4, 3×3 beats 9×1 for n=9).
//	  • ratioDiff  — prefer cells whose char-unit aspect ratio (cellW/cellH)
//	                 is close to 2.5, which is visually square given that a
//	                 terminal glyph is roughly twice as tall as wide in pixels.
//
// Typical results (w=160, h=50):
//
//	n=1→1×1  n=2→2×1  n=3→1×3  n=4→2×2  n=5→3×2
//	n=6→3×2  n=7→4×2  n=8→4×2  n=9→3×3  n=12→4×3
func gridColumns(w, h, n int) int {
	if n <= 1 {
		return 1
	}

	const (
		minCellW    = 24  // minimum usable cell width in chars
		minCellH    = 6   // minimum usable cell height in chars
		hintH       = 2   // hint bar at bottom (must match View's hintH)
		targetRatio = 2.5 // ideal cellW/cellH for content-friendly cells
	)

	availH := h - hintH
	if availH < minCellH {
		availH = minCellH
	}

	bestCols := 1
	bestScore := math.MaxFloat64

	for cols := 1; cols <= n; cols++ {
		rows := (n + cols - 1) / cols
		cellW := w / cols
		cellH := availH / rows

		// Skip layouts that produce cells too small to be useful.
		if cellW < minCellW || cellH < minCellH {
			continue
		}

		waste := cols*rows - n
		squareness := cols - rows
		if squareness < 0 {
			squareness = -squareness
		}
		ratioDiff := math.Abs(float64(cellW)/float64(cellH) - targetRatio)

		score := float64(waste)*5 + float64(squareness)*2 + ratioDiff
		if score < bestScore {
			bestScore = score
			bestCols = cols
		}
	}

	return bestCols
}
