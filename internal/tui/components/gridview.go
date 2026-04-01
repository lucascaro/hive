package components

import (
	"math"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/mux"
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
	Active   bool
	Cursor   int
	Width    int
	Height   int
	sessions []*state.Session
	contents map[string]string
}

// Show activates the grid with the given sessions.
func (gv *GridView) Show(sessions []*state.Session) {
	gv.Active = true
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

// Selected returns the currently focused session, or nil.
func (gv *GridView) Selected() *state.Session {
	if !gv.Active || gv.Cursor < 0 || gv.Cursor >= len(gv.sessions) {
		return nil
	}
	return gv.sessions[gv.Cursor]
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
	case "esc", "g", "q":
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
		}
	case "right", "l", "d":
		rowEnd := (gv.Cursor/cols)*cols + cols - 1
		if rowEnd >= n {
			rowEnd = n - 1
		}
		if gv.Cursor < rowEnd {
			gv.Cursor++
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
	hint := lipgloss.JoinVertical(
		lipgloss.Left,
		styles.MutedStyle.Render(styles.StatusLegend()),
		styles.MutedStyle.Render("←→↑↓/hjkl: navigate   enter/a: attach   x: kill   r: rename   G: all projects   esc/g/q: exit"),
	)
	out := lipgloss.JoinVertical(lipgloss.Left, grid, hint)

	// Hard-clamp to exactly gv.Height lines so that integer-division
	// remainder in cellH never leaves the grid 1..N lines short (which
	// causes old terminal content to show through at the bottom).
	outLines := strings.Count(out, "\n") + 1
	if outLines < gv.Height {
		out += strings.Repeat("\n"+strings.Repeat(" ", gv.Width), gv.Height-outLines)
	} else if outLines > gv.Height {
		// Trim excess lines from the bottom.
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

	// border=2cols, padding left+right=2 → inner width = w-4
	// border top+bottom=2rows + title row → inner content height = h-3
	innerW := w - 4
	innerH := h - 3
	if innerW < 4 {
		innerW = 4
	}
	if innerH < 1 {
		innerH = 1
	}

	// Title line — measure the prefix display width so that wide-char or
	// emoji session titles don't overflow the cell.
	dot := styles.StatusDot(string(sess.Status))
	badge := styles.AgentBadge(string(sess.AgentType))
	prefix := dot + " " + badge + " "
	maxTitleW := innerW - ansi.StringWidth(prefix)
	if maxTitleW < 0 {
		maxTitleW = 0
	}
	titleText := ansi.Truncate(sess.Title, maxTitleW, "…")
	titleLine := lipgloss.NewStyle().Width(innerW).Bold(selected).
		Render(prefix + titleText)

	// Content preview
	var contentStr string
	if content := gv.contents[sess.ID]; content != "" {
		lines := strings.Split(content, "\n")
		if len(lines) > innerH {
			lines = lines[len(lines)-innerH:]
		}
		for i, l := range lines {
			lines[i] = ansi.Truncate(l, innerW, "")
		}
		for len(lines) < innerH {
			lines = append(lines, "")
		}
		contentStr = strings.Join(lines, "\n")
	} else {
		contentStr = lipgloss.NewStyle().
			Width(innerW).Height(innerH).
			Foreground(styles.ColorMuted).
			Render("…")
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, titleLine, contentStr)
	return borderStyle.Width(w - 2).Height(h - 2).Render(inner)
}

// gridColumns computes the number of columns that best tiles n sessions inside
// a w×h terminal, maximising real-estate use.
//
// Scoring (lower is better, evaluated for each candidate column count):
//
//	score = waste×5  +  |cols−rows|×2  +  ratioDiff
//
//   • waste      — wasted cells (cols×rows − n); strongly penalised.
//   • |cols−rows| — prefer grids whose shape is close to square
//                   (e.g. 2×2 beats 4×1 for n=4, 3×3 beats 9×1 for n=9).
//   • ratioDiff  — prefer cells whose char-unit aspect ratio (cellW/cellH)
//                  is close to 2.5, which is visually square given that a
//                  terminal glyph is roughly twice as tall as wide in pixels.
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
		hintH       = 1   // hint bar at bottom
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
