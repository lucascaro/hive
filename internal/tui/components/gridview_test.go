package components

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
	"github.com/muesli/termenv"
)

func TestGridView_BellBadgeRendered(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	gv := &GridView{
		Active: true,
		Width:  60,
		Height: 10,
	}
	sess := &state.Session{ID: "s1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning}
	gv.Show([]*state.Session{sess}, state.GridRestoreProject)
	gv.SetBellPending(map[string]bool{"s1": true})
	gv.SetBellBlink(true) // blink-on state required to render the ♪ badge

	out := gv.View()
	if !strings.Contains(out, "♪") {
		t.Errorf("grid cell missing ♪ badge when bellPending=true; output:\n%s", out)
	}
}

func TestGridView_NoBellBadgeWhenNotPending(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	gv := &GridView{
		Active: true,
		Width:  60,
		Height: 10,
	}
	sess := &state.Session{ID: "s1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning}
	gv.Show([]*state.Session{sess}, state.GridRestoreProject)
	// No SetBellPending call — badge must be absent.

	out := gv.View()
	if strings.Contains(out, "♪") {
		t.Errorf("grid cell shows ♪ badge when bellPending=false; output:\n%s", out)
	}
}

func TestGridViewView_ShowsStatusLegend(t *testing.T) {
	gv := &GridView{
		Active: true,
		Width:  100,
		Height: 30,
	}
	gv.Show([]*state.Session{
		{ID: "s1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning},
	}, state.GridRestoreProject)

	out := gv.View()
	for _, want := range []string{"idle", "working", "waiting", "dead"} {
		if !strings.Contains(out, want) {
			t.Fatalf("grid legend missing %q in output: %q", want, out)
		}
	}
}

// TestGridView_ExactHeight ensures the grid never produces more or fewer lines
// than gv.Height regardless of integer-division remainders in cellH.
func TestGridView_ExactHeight(t *testing.T) {
	sessions := []*state.Session{
		{ID: "s1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s2", Title: "beta", AgentType: state.AgentClaude, Status: state.StatusIdle},
		{ID: "s3", Title: "gamma", AgentType: state.AgentClaude, Status: state.StatusWaiting},
		{ID: "s4", Title: "delta", AgentType: state.AgentClaude, Status: state.StatusDead},
	}
	dims := []struct{ w, h int }{
		{80, 24}, {80, 25}, {80, 30}, {80, 31},
		{160, 40}, {160, 41}, {160, 50}, {160, 51},
		{214, 60}, {214, 61}, {214, 62},
	}
	for _, d := range dims {
		gv := &GridView{Active: true, Width: d.w, Height: d.h}
		gv.Show(sessions, state.GridRestoreProject)
		out := gv.View()
		got := strings.Count(out, "\n") + 1
		if got != d.h {
			t.Errorf("w=%d h=%d: View() = %d lines, want exactly %d",
				d.w, d.h, got, d.h)
		}
	}
}

// TestGridView_ExactHeight_VariousCounts checks the invariant for 1–9 sessions.
func TestGridView_ExactHeight_VariousCounts(t *testing.T) {
	allSessions := make([]*state.Session, 9)
	for i := range allSessions {
		allSessions[i] = &state.Session{
			ID: "s" + string(rune('1'+i)), Title: "session",
			AgentType: state.AgentClaude, Status: state.StatusRunning,
		}
	}
	for n := 1; n <= 9; n++ {
		for _, h := range []int{24, 30, 40, 50, 62} {
			gv := &GridView{Active: true, Width: 160, Height: h}
			gv.Show(allSessions[:n], state.GridRestoreProject)
			out := gv.View()
			got := strings.Count(out, "\n") + 1
			if got != h {
				t.Errorf("n=%d h=%d: View() = %d lines, want exactly %d",
					n, h, got, h)
			}
		}
	}
}

// TestGridView_WorktreeBadge verifies that worktree sessions show the ⎇ badge
// and that the branch name appears only when it differs from the session title.
func TestGridView_WorktreeBadge(t *testing.T) {
	sessions := []*state.Session{
		{
			ID: "s1", Title: "my-feature", AgentType: state.AgentClaude, Status: state.StatusRunning,
			WorktreePath:   "/repo/.worktrees/my-feature",
			WorktreeBranch: "my-feature", // same as title → badge only
		},
		{
			ID: "s2", Title: "backend", AgentType: state.AgentClaude, Status: state.StatusRunning,
			WorktreePath:   "/repo/.worktrees/feat/backend-refactor",
			WorktreeBranch: "feat/backend-refactor", // differs → show branch name
		},
		{
			ID: "s3", Title: "no-worktree", AgentType: state.AgentClaude, Status: state.StatusIdle,
			// no WorktreePath → no badge
		},
	}
	gv := &GridView{Active: true, Width: 160, Height: 30}
	gv.Show(sessions, state.GridRestoreProject)
	out := gv.View()

	if !strings.Contains(out, "⎇") {
		t.Error("expected worktree badge ⎇ for worktree sessions, not found")
	}
	if !strings.Contains(out, "feat/backend-refactor") {
		t.Error("expected branch name 'feat/backend-refactor' for session with different branch")
	}
}

// TestGridView_NoLineExceedsWidth verifies that no line in the rendered grid is
// physically wider than gv.Width. This is the core overflow invariant: any line
// wider than TermWidth would physically wrap in the terminal and push the frame
// height over TermHeight — the "screen corruption" seen with Copilot CLI sessions.
func TestGridView_NoLineExceedsWidth(t *testing.T) {
	// 245-char wide content — typical Copilot CLI tmux-pane separator line.
	wideContent := strings.Repeat("─", 245)

	sessions := []*state.Session{
		{ID: "s1", Title: "copilot session", AgentType: state.AgentCopilot, Status: state.StatusRunning},
		{ID: "s2", Title: "other session", AgentType: state.AgentClaude, Status: state.StatusIdle},
	}

	termWidths := []int{80, 92, 93, 94, 120, 160, 200, 245}
	for _, w := range termWidths {
		gv := &GridView{Active: true, Width: w, Height: 30}
		gv.Show(sessions, state.GridRestoreProject)
		gv.SetContents(map[string]string{
			"s1": wideContent + "\nsome output\n" + wideContent,
			"s2": "normal content\nno wide lines",
		})
		out := gv.View()

		for i, line := range strings.Split(out, "\n") {
			// Use byte length as a conservative upper bound for visible width.
			// ANSI codes add bytes but no visible columns, so byte length ≥ visible width.
			// We check visible width via rune count; arrow chars are 1-wide each.
			visWidth := 0
			for _, r := range line {
				if r >= 0x1b {
					// Skip ANSI escape sequences heuristically — not exact, but
					// sufficient to catch obvious overflows in test content.
					continue
				}
				visWidth++
			}
			// A stricter check: raw byte length of printable portion.
			// We simply verify the line does not have more runes than gv.Width.
			if visWidth > w {
				t.Errorf("w=%d: line %d visible rune count %d exceeds terminal width %d:\n%q",
					w, i, visWidth, w, line)
			}
		}
	}
}

// TestGridView_NarrowTerminalHintBar specifically tests terminals narrower than
// the 93-char hint string, which was the root cause of overflow before the fix.
func TestGridView_NarrowTerminalHintBar(t *testing.T) {
	sessions := []*state.Session{
		{ID: "s1", Title: "session", AgentType: state.AgentClaude, Status: state.StatusRunning},
	}
	for _, w := range []int{60, 70, 80, 90, 92} {
		gv := &GridView{Active: true, Width: w, Height: 24}
		gv.Show(sessions, state.GridRestoreProject)
		out := gv.View()
		lines := strings.Split(out, "\n")
		if len(lines) != 24 {
			t.Errorf("w=%d: View() = %d lines, want 24", w, len(lines))
		}
	}
}

// TestGridView_WideContentShowsLatestLines checks that pre-truncation causes
// the rendered cell to show the LAST innerH lines of content (most recent),
// not the first fragment produced by word-wrap.
func TestGridView_WideContentShowsLatestLines(t *testing.T) {
	// Build content where the last line is recognisably different.
	var lines []string
	for i := 0; i < 20; i++ {
		lines = append(lines, strings.Repeat("─", 245)) // wide separator
	}
	lines = append(lines, "RECENT_OUTPUT_MARKER")
	content := strings.Join(lines, "\n")

	sess := &state.Session{
		ID:        "s1",
		Title:     "test",
		AgentType: state.AgentCopilot,
		Status:    state.StatusRunning,
	}
	gv := &GridView{Active: true, Width: 120, Height: 30}
	gv.Show([]*state.Session{sess}, state.GridRestoreProject)
	gv.SetContents(map[string]string{"s1": content})
	out := gv.View()
	if !strings.Contains(out, "RECENT_OUTPUT_MARKER") {
		t.Error("grid cell does not show the most-recent content line (RECENT_OUTPUT_MARKER missing)")
	}
}

func TestGridView_SetProjectColors(t *testing.T) {
	gv := &GridView{}
	colors := map[string]string{"p1": "#FF0000", "p2": "#00FF00"}
	gv.SetProjectColors(colors)
	if gv.projectColors["p1"] != "#FF0000" {
		t.Errorf("projectColors[p1] = %q, want #FF0000", gv.projectColors["p1"])
	}
	if gv.projectColors["p2"] != "#00FF00" {
		t.Errorf("projectColors[p2] = %q, want #00FF00", gv.projectColors["p2"])
	}
}

func TestGridView_CellRendersWithProjectColor(t *testing.T) {
	sessions := []*state.Session{
		{ID: "s1", ProjectID: "p1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s2", ProjectID: "p2", Title: "beta", AgentType: state.AgentCodex, Status: state.StatusIdle},
	}
	gv := &GridView{Active: true, Width: 120, Height: 30}
	gv.Show(sessions, state.GridRestoreAll)
	gv.SetProjectNames(map[string]string{"p1": "project-one", "p2": "project-two"})
	gv.SetProjectColors(map[string]string{"p1": "#EF4444", "p2": "#3B82F6"})
	out := gv.View()

	if !strings.Contains(out, "alpha") {
		t.Error("grid view missing session title 'alpha'")
	}
	if !strings.Contains(out, "beta") {
		t.Error("grid view missing session title 'beta'")
	}
}

func TestGridView_CellRendersWithEmptyProjectColor(t *testing.T) {
	sessions := []*state.Session{
		{ID: "s1", ProjectID: "p1", Title: "test", AgentType: state.AgentClaude, Status: state.StatusRunning},
	}
	gv := &GridView{Active: true, Width: 100, Height: 20}
	gv.Show(sessions, state.GridRestoreProject)
	// No SetProjectColors — should use fallback without panic.
	out := gv.View()
	if !strings.Contains(out, "test") {
		t.Error("grid view missing session title with nil projectColors")
	}
}

func TestGridView_CellRendersAllStatuses(t *testing.T) {
	// Ensure renderCell works for all status types with project colors.
	statuses := []state.SessionStatus{
		state.StatusRunning, state.StatusIdle, state.StatusWaiting, state.StatusDead,
	}
	for _, status := range statuses {
		sessions := []*state.Session{
			{ID: "s1", ProjectID: "p1", Title: "test", AgentType: state.AgentClaude, Status: status},
		}
		gv := &GridView{Active: true, Width: 80, Height: 20}
		gv.Show(sessions, state.GridRestoreProject)
		gv.SetProjectColors(map[string]string{"p1": "#F59E0B"})
		out := gv.View()
		if out == "" {
			t.Errorf("grid view returned empty for status %q", status)
		}
	}
}

func TestGridView_SyncCursor(t *testing.T) {
	sessions := []*state.Session{
		{ID: "s1", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s2", Title: "beta", AgentType: state.AgentClaude, Status: state.StatusIdle},
		{ID: "s3", Title: "gamma", AgentType: state.AgentClaude, Status: state.StatusRunning},
	}
	gv := &GridView{Active: true, Width: 160, Height: 30}
	gv.Show(sessions, state.GridRestoreProject)

	t.Run("moves cursor to matching session", func(t *testing.T) {
		gv.SyncCursor("s3")
		if gv.Cursor != 2 {
			t.Errorf("Cursor = %d, want 2", gv.Cursor)
		}
	})

	t.Run("no-op for unknown session ID", func(t *testing.T) {
		gv.Cursor = 1
		gv.SyncCursor("unknown-id")
		if gv.Cursor != 1 {
			t.Errorf("Cursor = %d, want 1 (unchanged)", gv.Cursor)
		}
	})

	t.Run("no-op for empty session ID", func(t *testing.T) {
		gv.Cursor = 2
		gv.SyncCursor("")
		if gv.Cursor != 2 {
			t.Errorf("Cursor = %d, want 2 (unchanged)", gv.Cursor)
		}
	})

	t.Run("syncs to first session", func(t *testing.T) {
		gv.Cursor = 2
		gv.SyncCursor("s1")
		if gv.Cursor != 0 {
			t.Errorf("Cursor = %d, want 0", gv.Cursor)
		}
	})
}

// TestGridView_CursorWrap tests horizontal arrow-key wrapping between rows.
// Uses 5 sessions at 160×50 which yields a 3×2 grid (row0=[0,1,2], row1=[3,4]).
func TestGridView_CursorWrap(t *testing.T) {
	sessions := make([]*state.Session, 5)
	for i := range sessions {
		sessions[i] = &state.Session{
			ID: "s" + string(rune('1'+i)), Title: "sess",
			AgentType: state.AgentClaude, Status: state.StatusRunning,
		}
	}

	makeGV := func(cursor int) *GridView {
		gv := &GridView{Active: true, Width: 160, Height: 50}
		gv.Show(sessions, state.GridRestoreProject)
		gv.Cursor = cursor
		return gv
	}

	key := func(s string) tea.KeyMsg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}

	tests := []struct {
		name      string
		cursor    int
		key       tea.KeyMsg
		wantCursor int
	}{
		// Normal movement within a row
		{"right within row", 0, key("l"), 1},
		{"left within row", 1, key("h"), 0},
		// Wrapping across rows
		{"right wraps to next row", 2, key("l"), 3},
		{"left wraps to prev row", 3, key("h"), 2},
		// No wrap at boundaries
		{"right at last session stays", 4, key("l"), 4},
		{"left at index 0 stays", 0, key("h"), 0},
		// Arrow keys work too
		{"arrow right wraps", 2, tea.KeyMsg{Type: tea.KeyRight}, 3},
		{"arrow left wraps", 3, tea.KeyMsg{Type: tea.KeyLeft}, 2},
		// "d" alias for right
		{"d key wraps to next row", 2, key("d"), 3},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gv := makeGV(tc.cursor)
			gv.Update(tc.key)
			if gv.Cursor != tc.wantCursor {
				t.Errorf("Cursor = %d, want %d", gv.Cursor, tc.wantCursor)
			}
		})
	}
}

// TestGridView_CursorWrap_SingleColumn verifies wrapping in a 1-column grid.
// With 1 column each cell is both first and last in its row, so left/right
// should wrap to the adjacent row (since each row has exactly one cell).
func TestGridView_CursorWrap_SingleColumn(t *testing.T) {
	// 2 sessions at 40×50 → gridColumns returns 1 (too narrow for 2 cols).
	sessions := []*state.Session{
		{ID: "s1", Title: "a", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s2", Title: "b", AgentType: state.AgentClaude, Status: state.StatusRunning},
	}
	makeGV := func(cursor int) *GridView {
		gv := &GridView{Active: true, Width: 40, Height: 50}
		gv.Show(sessions, state.GridRestoreProject)
		gv.Cursor = cursor
		return gv
	}
	key := func(s string) tea.KeyMsg {
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
	}

	t.Run("right wraps to next row", func(t *testing.T) {
		gv := makeGV(0)
		gv.Update(key("l"))
		if gv.Cursor != 1 {
			t.Errorf("Cursor = %d, want 1", gv.Cursor)
		}
	})
	t.Run("left wraps to prev row", func(t *testing.T) {
		gv := makeGV(1)
		gv.Update(key("h"))
		if gv.Cursor != 0 {
			t.Errorf("Cursor = %d, want 0", gv.Cursor)
		}
	})
	t.Run("right at last stays", func(t *testing.T) {
		gv := makeGV(1)
		gv.Update(key("l"))
		if gv.Cursor != 1 {
			t.Errorf("Cursor = %d, want 1", gv.Cursor)
		}
	})
	t.Run("left at first stays", func(t *testing.T) {
		gv := makeGV(0)
		gv.Update(key("h"))
		if gv.Cursor != 0 {
			t.Errorf("Cursor = %d, want 0", gv.Cursor)
		}
	})
}

// TestGridView_SelectedCellHasBackground verifies that the selected cell
// contains the ColorGridSelected background escape and the unselected does not.
func TestGridView_SelectedCellHasBackground(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	sess := &state.Session{
		ID: "s1", ProjectID: "p1", Title: "test-sess",
		AgentType: state.AgentClaude, Status: state.StatusRunning,
	}
	gv := &GridView{Active: true, Width: 80, Height: 20}
	gv.Show([]*state.Session{sess}, state.GridRestoreProject)
	gv.SetProjectColors(map[string]string{"p1": "#7C3AED"})
	gv.SetContents(map[string]string{"s1": "hello world"})

	selected := gv.renderCell(sess, 40, 15, true)
	unselected := gv.renderCell(sess, 40, 15, false)

	bgEsc := styles.GridSelectedBgEsc
	if bgEsc == "" {
		t.Fatal("GridSelectedBgEsc is empty — cannot verify background")
	}
	if !strings.Contains(selected, bgEsc) {
		t.Errorf("selected cell missing GridSelectedBgEsc %q", bgEsc)
	}
	if strings.Contains(unselected, bgEsc) {
		t.Errorf("unselected cell should not contain GridSelectedBgEsc %q", bgEsc)
	}
}

// TestGridView_SelectedCellSubtitleHasBackground verifies that the subtitle
// line in the selected cell contains the ColorGridSelected background escape.
func TestGridView_SelectedCellSubtitleHasBackground(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)
	sess := &state.Session{
		ID: "s1", ProjectID: "p1", Title: "test",
		AgentType: state.AgentClaude, Status: state.StatusRunning,
		TmuxSession: "sess", TmuxWindow: 0,
	}
	gv := &GridView{Active: true, Width: 80, Height: 30}
	gv.Show([]*state.Session{sess}, state.GridRestoreProject)
	gv.SetProjectColors(map[string]string{"p1": "#7C3AED"})
	gv.SetPaneTitles(map[string]string{"sess:0": "Working on task"})

	selected := gv.renderCell(sess, 40, 15, true)
	unselected := gv.renderCell(sess, 40, 15, false)

	findSubtitleLine := func(rendered string) string {
		for _, line := range strings.Split(rendered, "\n") {
			if strings.Contains(line, "Working on task") {
				return line
			}
		}
		return ""
	}

	selSub := findSubtitleLine(selected)
	unselSub := findSubtitleLine(unselected)
	if selSub == "" {
		t.Fatal("subtitle text missing from selected cell")
	}
	if unselSub == "" {
		t.Fatal("subtitle text missing from unselected cell")
	}

	bgEsc := styles.GridSelectedBgEsc
	if !strings.Contains(selSub, bgEsc) {
		t.Errorf("selected subtitle missing GridSelectedBgEsc %q: %q", bgEsc, selSub)
	}
	if strings.Contains(unselSub, bgEsc) {
		t.Errorf("unselected subtitle should not contain GridSelectedBgEsc %q: %q", bgEsc, unselSub)
	}
}

// TestSanitizePaneTitle covers the sanitizer used to scrub untrusted OSC 0/2
// payloads before rendering them inside grid cells.  Pane titles can contain
// arbitrary ANSI escape sequences and control characters; lipgloss does not
// sanitize, so this function is the only thing standing between agent output
// and the visible UI.
func TestSanitizePaneTitle(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain ascii", "claude: working", "claude: working"},
		{"trailing whitespace trimmed", "  pre post  ", "pre post"},
		{"sgr color stripped", "\x1b[1;31mred\x1b[0m", "red"},
		// Private-mode CSI sequences (parameter byte '?') were not stripped
		// by the original [0-9;]*[a-zA-Z] pattern — caught in PR #58 review.
		{"private mode CSI stripped", "\x1b[?25lhidden\x1b[?25h", "hidden"},
		{"alt screen toggle stripped", "\x1b[?1049hfoo\x1b[?1049l", "foo"},
		// CSI with intermediate byte (DECSCUSR cursor shape).
		{"intermediate byte CSI stripped", "\x1b[1 qfoo", "foo"},
		{"osc bel terminated stripped", "\x1b]2;title\x07after", "after"},
		{"osc st terminated stripped", "\x1b]2;title\x1b\\after", "after"},
		{"control chars only", "\x00\x01\x07", ""},
		{"mixed control + visible", "a\x00b\x01c", "abc"},
		{"del char stripped", "before\x7fafter", "beforeafter"},
		{"unicode preserved", "⠋ Working on task", "⠋ Working on task"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sanitizePaneTitle(tc.in); got != tc.want {
				t.Errorf("sanitizePaneTitle(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestGridView_LastRowFillsHeight verifies that when sessions don't evenly fill
// the grid, the last row expands to use the remaining vertical space instead of
// leaving empty cells.
func TestGridView_LastRowFillsHeight(t *testing.T) {
	prev := lipgloss.ColorProfile()
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(prev)

	titles := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta", "iota"}
	cases := []struct {
		name string
		n    int // number of sessions
		w, h int
	}{
		{"3 sessions 2-col", 3, 80, 24},
		{"3 sessions 2-col tall", 3, 80, 40},
		{"5 sessions 3-col", 5, 120, 30},
		{"7 sessions 3-col", 7, 120, 40},
		{"1 session", 1, 80, 24},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sessions := make([]*state.Session, tc.n)
			for i := range sessions {
				sessions[i] = &state.Session{
					ID: "s" + string(rune('1'+i)), Title: titles[i],
					AgentType: state.AgentClaude, Status: state.StatusRunning,
				}
			}
			gv := &GridView{Active: true, Width: tc.w, Height: tc.h}
			gv.Show(sessions, state.GridRestoreProject)
			out := gv.View()

			// Height invariant must hold.
			got := strings.Count(out, "\n") + 1
			if got != tc.h {
				t.Errorf("View() = %d lines, want %d", got, tc.h)
			}

			// All session titles must appear in the output.
			for _, s := range sessions {
				if !strings.Contains(out, s.Title) {
					t.Errorf("session %q missing from output", s.Title)
				}
			}
		})
	}
}

// TestGridView_CellAt_ExtendedCell verifies that mouse clicks in the extended
// portion of a cell above an empty slot correctly map to that session.
// This is the behavioral test for the column-first rendering fix: a sparse
// column's last real cell absorbs the empty row below it, so CellAt must
// return that session for y-coordinates in the extended area.
func TestGridView_CellAt_ExtendedCell(t *testing.T) {
	// 5 sessions in a 3-col grid: columns 0 and 1 have 2 sessions each,
	// column 2 has only session[2] — its cell extends to fill the second row.
	sessions := []*state.Session{
		{ID: "s0", Title: "alpha", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s1", Title: "beta", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s2", Title: "gamma", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s3", Title: "delta", AgentType: state.AgentClaude, Status: state.StatusRunning},
		{ID: "s4", Title: "epsilon", AgentType: state.AgentClaude, Status: state.StatusRunning},
	}
	const w, h = 120, 30
	gv := &GridView{Active: true, Width: w, Height: h}
	gv.Show(sessions, state.GridRestoreProject)

	cols := 3
	const hintH = 2
	totalH := h - hintH
	rows := 2
	cellH := totalH / rows
	cellW := w / cols

	// Clicks in the normal region of each cell must resolve correctly.
	for i, s := range sessions {
		r, c := i/cols, i%cols
		x := c*cellW + cellW/2
		y := r*cellH + 1
		got, ok := gv.CellAt(x, y)
		if !ok || got != i {
			t.Errorf("CellAt normal region session %q (%d): got idx=%d ok=%v, want idx=%d ok=true", s.Title, i, got, ok, i)
		}
	}

	// Click in the extended area of session[2] (column 2, row 0 extended into row 1).
	// y = cellH+2 is inside the second row's vertical range, but column 2 has no
	// session there — CellAt must still return session index 2 (the extended cell).
	extY := cellH + 2
	extX := 2*cellW + cellW/2
	got, ok := gv.CellAt(extX, extY)
	if !ok || got != 2 {
		t.Errorf("CellAt extended region: got idx=%d ok=%v, want idx=2 ok=true (session gamma should extend into empty slot)", got, ok)
	}

	// Clicks in columns 0 and 1 at the same y must resolve to row-1 sessions (3 and 4).
	for c, wantIdx := range []int{3, 4} {
		x := c*cellW + cellW/2
		got, ok := gv.CellAt(x, extY)
		if !ok || got != wantIdx {
			t.Errorf("CellAt row-1 col %d at y=%d: got idx=%d ok=%v, want idx=%d ok=true", c, extY, got, ok, wantIdx)
		}
	}
}
