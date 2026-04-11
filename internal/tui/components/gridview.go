package components

import (
	"math"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// GridSessionSelectedMsg is sent when the user selects a session in the grid.
type GridSessionSelectedMsg struct {
	TmuxSession string
	TmuxWindow  int
}

// GridPreviewsUpdatedMsg carries fresh capture-pane content for all sessions.
type GridPreviewsUpdatedMsg struct {
	Contents map[string]string
}

// PollGridPreviews returns a tea.Cmd that captures pane content for all sessions.
func PollGridPreviews(sessions []*state.Session, interval time.Duration) tea.Cmd {
	return tea.Tick(interval, func(_ time.Time) tea.Msg {
		contents := make(map[string]string, len(sessions))
		for _, sess := range sessions {
			if sess.TmuxSession == "" {
				continue
			}
			target := mux.Target(sess.TmuxSession, sess.TmuxWindow)
			content, err := mux.CapturePane(target, 200)
			if err == nil {
				contents[sess.ID] = sanitizePreviewContent(content)
			}
		}
		return GridPreviewsUpdatedMsg{Contents: contents}
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
	contents      map[string]string
	projectNames  map[string]string // projectID → display name
	projectColors map[string]string // projectID → hex color
	sessionColors map[string]string // sessionID → hex color
	paneTitles    map[string]string // target ("tmuxSession:windowIdx") → live pane title
}

// Show activates the grid with the given sessions.
func (gv *GridView) Show(sessions []*state.Session, mode state.GridRestoreMode) {
	gv.Active = true
	gv.Mode = mode
	gv.sessions = sessions
	if gv.Cursor >= len(sessions) {
		gv.Cursor = 0
	}
	if gv.contents == nil {
		gv.contents = make(map[string]string)
	}
}

// Hide deactivates the grid.
func (gv *GridView) Hide() { gv.Active = false }

// SetContents updates the captured preview content map.
func (gv *GridView) SetContents(contents map[string]string) {
	gv.contents = contents
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

// Selected returns the currently focused session, or nil.
func (gv *GridView) Selected() *state.Session {
	if !gv.Active || gv.Cursor < 0 || gv.Cursor >= len(gv.sessions) {
		return nil
	}
	return gv.sessions[gv.Cursor]
}

// SyncCursor moves the cursor to the session matching sessionID.
// No-op if sessionID is empty or not found in the current sessions slice.
func (gv *GridView) SyncCursor(sessionID string) {
	if sessionID == "" {
		return
	}
	for i, sess := range gv.sessions {
		if sess.ID == sessionID {
			gv.Cursor = i
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
	n := len(gv.sessions)
	cols := gridColumns(gv.Width, gv.Height, n)

	switch msg.String() {
	case "esc", "q":
		gv.Hide()
	case "enter", "a":
		if sess := gv.Selected(); sess != nil {
			gv.Hide()
			s := sess
			return func() tea.Msg {
				return GridSessionSelectedMsg{TmuxSession: s.TmuxSession, TmuxWindow: s.TmuxWindow}
			}, true
		}
	case "left", "h":
		rowStart := (gv.Cursor / cols) * cols
		if gv.Cursor > rowStart {
			gv.Cursor--
		} else if gv.Cursor > 0 {
			gv.Cursor-- // wrap to last cell of previous row
		}
	case "right", "l", "d":
		rowEnd := (gv.Cursor/cols)*cols + cols - 1
		if rowEnd >= n {
			rowEnd = n - 1
		}
		if gv.Cursor < rowEnd {
			gv.Cursor++
		} else if gv.Cursor < n-1 {
			gv.Cursor++ // wrap to first cell of next row
		}
	case "up", "k", "w":
		if gv.Cursor >= cols {
			gv.Cursor -= cols
		}
	case "down", "j", "s":
		if gv.Cursor+cols < n {
			gv.Cursor += cols
		}
	}
	return nil, true // consume all keys while grid is active
}

// View renders the full-screen grid.
func (gv *GridView) View() string {
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
	cellH := (gv.Height - hintH) / rows
	if cellH < 5 {
		cellH = 5
	}

	var rowViews []string
	for r := 0; r < rows; r++ {
		var cellViews []string
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			if idx >= n {
				cellViews = append(cellViews, lipgloss.NewStyle().Width(cellW).Height(cellH).Render(""))
				continue
			}
			cellViews = append(cellViews, gv.renderCell(gv.sessions[idx], cellW, cellH, idx == gv.Cursor))
		}
		rowViews = append(rowViews, lipgloss.JoinHorizontal(lipgloss.Top, cellViews...))
	}

	grid := lipgloss.JoinVertical(lipgloss.Left, rowViews...)
	// Truncate hint lines to gv.Width before joining with the grid.
	// Without this, JoinVertical computes maxWidth = max(grid_width, hint_width).
	// When hint_width (93 display chars) exceeds gv.Width (narrow terminals,
	// 60–92 cols), every grid row is padded to hint_width > TermWidth, causing
	// physical terminal line-wrap even though logical line count is correct.
	hintLine1 := ansi.Truncate(styles.MutedStyle.Render(styles.StatusLegend()), gv.Width, "")
	hintLine2 := ansi.Truncate(styles.MutedStyle.Render("←→↑↓/hjkl: navigate   S-←/→: reorder   enter/a: attach   x: kill   r: rename   c/C: color   v/V: session color   G: all   esc/g/q: exit"), gv.Width, "")
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

func (gv *GridView) renderCell(sess *state.Session, w, h int, selected bool) string {
	borderColor := styles.ColorBorder
	if selected {
		borderColor = styles.ColorAccent
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
	bg := lipgloss.Color(projColor)
	fg := styles.ContrastForeground(projColor)

	// Keep the status dot and agent badge on a dark background so their
	// bright foreground colors remain legible regardless of project color.
	darkBg := lipgloss.Color(styles.ColorBg)
	dot := styles.StatusDotOnBg(string(sess.Status), darkBg)
	badge := styles.AgentBadgeOnBg(string(sess.AgentType), darkBg)
	darkSp := lipgloss.NewStyle().Background(darkBg).Render(" ")
	bgSp := lipgloss.NewStyle().Background(bg).Foreground(fg).Render(" ")
	prefixStr := dot + darkSp + badge + bgSp
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
	textPortion := titleStr + suffix
	textPortionW := ansi.StringWidth(prefixStr) // already measured
	actualTextW := ansi.StringWidth(textPortion)
	remainW := innerW - textPortionW
	if pad := remainW - actualTextW; pad > 0 {
		textPortion += strings.Repeat(" ", pad)
	}
	// Render with gradient if the session has its own color; flat otherwise.
	sessColor := gv.sessionColors[sess.ID]
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
			subtitleLine = lipgloss.NewStyle().
				Foreground(styles.ColorMuted).
				Italic(true).
				Width(innerW).MaxWidth(innerW).
				Render(trunc)
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
	var contentStr string
	if content := gv.contents[sess.ID]; content != "" {
		rawLines := strings.Split(lastNLines(content, innerH), "\n")
		for i, l := range rawLines {
			rawLines[i] = ansi.Truncate(l, innerW, "")
		}
		contentStr = lipgloss.NewStyle().
			Width(innerW).Height(innerH).
			MaxWidth(innerW).MaxHeight(innerH).
			Render(strings.Join(rawLines, "\n"))
	} else {
		contentStr = lipgloss.NewStyle().
			Width(innerW).Height(innerH).
			MaxWidth(innerW).MaxHeight(innerH).
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

// MoveUp moves the grid cursor up by one row.
func (gv *GridView) MoveUp() {
	cols := gridColumns(gv.Width, gv.Height, len(gv.sessions))
	if gv.Cursor >= cols {
		gv.Cursor -= cols
	}
}

// MoveDown moves the grid cursor down by one row.
func (gv *GridView) MoveDown() {
	n := len(gv.sessions)
	cols := gridColumns(gv.Width, gv.Height, n)
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
	cellH := (gv.Height - hintH) / rows
	if cellH < 5 {
		cellH = 5
	}
	// Clicks in the hint bar at the bottom are ignored.
	if y >= gv.Height-hintH {
		return -1, false
	}
	col := x / cellW
	row := y / cellH
	if col >= cols || row >= rows {
		return -1, false
	}
	i := row*cols + col
	if i >= n {
		return -1, false
	}
	return i, true
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
