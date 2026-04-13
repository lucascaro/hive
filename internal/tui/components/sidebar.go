package components

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

var (
	sidebarLog     = log.New(os.Stderr, "[sidebar] ", log.Ltime)
	sidebarLogOnce sync.Once
)

// InitSidebarLog upgrades the sidebar logger from stderr to the hive log file.
// Called once from tui.New() so the log path is resolved after env overrides.
func InitSidebarLog() {
	sidebarLogOnce.Do(func() {
		f, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		sidebarLog = log.New(f, "[sidebar] ", log.Ltime|log.Lmicroseconds)
	})
}

// ItemKind identifies the type of a sidebar row.
type ItemKind int

const (
	KindProject ItemKind = iota
	KindTeam
	KindSession
)

// SidebarItem is a flattened row in the sidebar tree.
type SidebarItem struct {
	Kind      ItemKind
	ProjectID string
	TeamID    string
	SessionID string
	Label     string
	Indent    int
	// Rendered fields
	AgentType      string
	Status         string
	TeamRole       string
	Collapsed      bool
	ProjectNum     int    // 1-based number for projects (0 = not a project)
	IsWorktree     bool   // true if the session runs in a git worktree
	WorktreeBranch string // branch name for worktree sessions
	ProjectColor   string // hex color of the parent project
	SessionColor   string // hex color of the session (empty = inherit project)
}

// Sidebar manages the project/team/session tree.
type Sidebar struct {
	Items        []SidebarItem
	Cursor       int
	Width        int
	Height       int
	FilterQuery  string
	ScrollOffset int // index of first visible item (for scrolling)
	bellPending  map[string]bool // sessionID → true when a bell has fired and user hasn't attached yet
	bellBlinkOn  bool            // toggled by the bell-blink ticker; true = show ♪, false = show status dot
}

// SetBellPending updates the set of sessions with pending bell indicators.
func (s *Sidebar) SetBellPending(bells map[string]bool) {
	s.bellPending = bells
}

// SetBellBlink updates the animated on/off state for the bell badge.
func (s *Sidebar) SetBellBlink(on bool) {
	s.bellBlinkOn = on
}

// Rebuild recomputes the flat item list from state, applying filter.
func (s *Sidebar) Rebuild(appState *state.AppState) {
	s.Items = nil
	projectNum := 0
	for _, p := range appState.Projects {
		if s.FilterQuery != "" && !strings.Contains(strings.ToLower(p.Name), strings.ToLower(s.FilterQuery)) {
			// Still include if any child matches.
			if !projectMatchesFilter(p, s.FilterQuery) {
				continue
			}
		}
		projectNum++
		collapsed := p.Collapsed
		s.Items = append(s.Items, SidebarItem{
			Kind:         KindProject,
			ProjectID:    p.ID,
			Label:        p.Name,
			Collapsed:    collapsed,
			ProjectNum:   projectNum,
			ProjectColor: p.Color,
		})
		if collapsed {
			continue
		}
		// Teams
		for _, t := range p.Teams {
			s.Items = append(s.Items, SidebarItem{
				Kind:         KindTeam,
				ProjectID:    p.ID,
				TeamID:       t.ID,
				Label:        t.Name,
				Indent:       1,
				Collapsed:    t.Collapsed,
				Status:       string(t.TeamStatus()),
				ProjectColor: p.Color,
			})
			if t.Collapsed {
				continue
			}
			for _, sess := range t.Sessions {
				if s.FilterQuery != "" && !matchesSessionFilter(sess, s.FilterQuery) {
					continue
				}
				s.Items = append(s.Items, SidebarItem{
					Kind:           KindSession,
					ProjectID:      p.ID,
					TeamID:         t.ID,
					SessionID:      sess.ID,
					Label:          sess.Title,
					Indent:         2,
					AgentType:      string(sess.AgentType),
					Status:         string(sess.Status),
					TeamRole:       string(sess.TeamRole),
					IsWorktree:     sess.WorktreePath != "",
					WorktreeBranch: sess.WorktreeBranch,
					ProjectColor:   p.Color,
					SessionColor:   sess.Color,
				})
			}
		}
		// Standalone sessions
		for _, sess := range p.Sessions {
			if s.FilterQuery != "" && !matchesSessionFilter(sess, s.FilterQuery) {
				continue
			}
			s.Items = append(s.Items, SidebarItem{
				Kind:           KindSession,
				ProjectID:      p.ID,
				SessionID:      sess.ID,
				Label:          sess.Title,
				Indent:         1,
				AgentType:      string(sess.AgentType),
				Status:         string(sess.Status),
				TeamRole:       string(state.RoleStandalone),
				IsWorktree:     sess.WorktreePath != "",
				WorktreeBranch: sess.WorktreeBranch,
				ProjectColor:   p.Color,
				SessionColor:   sess.Color,
			})
		}
	}
	// Clamp cursor. Only sync to the active session when the cursor was out of
	// bounds (e.g. a session was removed), so background rebuilds don't steal
	// focus from project/team rows the user is navigating.
	if s.Cursor >= len(s.Items) {
		s.Cursor = max(0, len(s.Items)-1)
		if appState.ActiveSessionID != "" {
			s.SyncActiveSession(appState.ActiveSessionID)
		}
	}
}

// Selected returns the currently highlighted SidebarItem, or nil.
func (s *Sidebar) Selected() *SidebarItem {
	if len(s.Items) == 0 || s.Cursor >= len(s.Items) {
		return nil
	}
	item := s.Items[s.Cursor]
	return &item
}

// EnsureCursorVisible adjusts ScrollOffset so the cursor row is within the
// visible window. h is the total sidebar height (including the 2-row header).
func (s *Sidebar) EnsureCursorVisible(h int) {
	visible := h - 2 // rows available for items (minus title + blank line)
	if visible < 1 {
		visible = 1
	}
	if s.Cursor < s.ScrollOffset {
		s.ScrollOffset = s.Cursor
	} else if s.Cursor >= s.ScrollOffset+visible {
		s.ScrollOffset = s.Cursor - visible + 1
	}
	maxOffset := len(s.Items) - visible
	if maxOffset < 0 {
		maxOffset = 0
	}
	if s.ScrollOffset > maxOffset {
		s.ScrollOffset = maxOffset
	}
	if s.ScrollOffset < 0 {
		s.ScrollOffset = 0
	}
}

// ItemAtRow returns the item index that corresponds to a click at terminal row y.
// Returns -1 if the row does not map to a valid item (e.g. header or out of range).
func (s *Sidebar) ItemAtRow(y int) int {
	const headerRows = 2 // title + blank line
	if y < headerRows {
		return -1
	}
	idx := s.ScrollOffset + (y - headerRows)
	if idx < 0 || idx >= len(s.Items) {
		return -1
	}
	return idx
}

// MoveUp moves the cursor up one item.
func (s *Sidebar) MoveUp() {
	if s.Cursor > 0 {
		s.Cursor--
	}
	s.EnsureCursorVisible(s.Height)
}

// MoveDown moves the cursor down one item.
func (s *Sidebar) MoveDown() {
	if s.Cursor < len(s.Items)-1 {
		s.Cursor++
	}
	s.EnsureCursorVisible(s.Height)
}

// JumpPrevProject moves cursor to the previous project row.
func (s *Sidebar) JumpPrevProject() {
	for i := s.Cursor - 1; i >= 0; i-- {
		if s.Items[i].Kind == KindProject {
			s.Cursor = i
			return
		}
	}
}

// JumpNextProject moves cursor to the next project row.
func (s *Sidebar) JumpNextProject() {
	for i := s.Cursor + 1; i < len(s.Items); i++ {
		if s.Items[i].Kind == KindProject {
			s.Cursor = i
			return
		}
	}
}

// SyncActiveSession sets the cursor to the active session in appState.
func (s *Sidebar) SyncActiveSession(activeSessionID string) {
	for i, item := range s.Items {
		if item.SessionID == activeSessionID {
			s.Cursor = i
			return
		}
	}
}

// View renders the sidebar content.
func (s *Sidebar) View(activeSessionID string, focused bool) string {
	if s.Width <= 0 {
		return ""
	}
	innerW := s.Width - 2 // account for border
	if innerW < 1 {
		innerW = 1
	}

	// Ensure scroll offset is in sync with current state before rendering.
	s.EnsureCursorVisible(s.Height)

	// Title header
	titleLine := styles.TitleStyle.Render(" hive")
	rows := []string{titleLine, ""}

	// Only render the visible window of items (from ScrollOffset).
	visible := s.Height - 2
	if visible < 1 {
		visible = 1
	}
	end := s.ScrollOffset + visible
	if end > len(s.Items) {
		end = len(s.Items)
	}
	visibleItems := s.Items[s.ScrollOffset:end]

	for i, item := range visibleItems {
		rows = append(rows, s.renderItem(item, i+s.ScrollOffset == s.Cursor, item.SessionID == activeSessionID, innerW))
	}
	if len(s.Items) == 0 {
		rows = append(rows, styles.MutedStyle.Render("  No sessions yet"))
		rows = append(rows, styles.MutedStyle.Render("  Press n to create a project"))
	}

	content := strings.Join(rows, "\n")
	// Truncate to height
	lines := strings.Split(content, "\n")
	rawLines := len(lines)
	if len(lines) > s.Height {
		lines = lines[:s.Height]
	}
	content = strings.Join(lines, "\n")

	borderColor := styles.ColorBorder
	if focused {
		borderColor = styles.ColorAccent
	}
	rendered := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, true, false, false).
		BorderForeground(borderColor).
		Width(s.Width - 1).
		Height(s.Height).
		Render(content)
	renderedLines := strings.Count(rendered, "\n") + 1
	sidebarLog.Printf("View: w=%d h=%d innerW=%d items=%d cursor=%d scrollOffset=%d rawContentLines=%d afterTruncate=%d rendered=%d%s",
		s.Width, s.Height, innerW, len(s.Items), s.Cursor, s.ScrollOffset,
		rawLines, len(lines), renderedLines,
		func() string {
			if renderedLines != s.Height {
				return fmt.Sprintf(" HEIGHT_MISMATCH(want=%d got=%d)", s.Height, renderedLines)
			}
			return ""
		}(),
	)
	return rendered
}

func (s *Sidebar) renderItem(item SidebarItem, selected, active bool, width int) string {
	indent := strings.Repeat("  ", item.Indent)
	var prefix, label string
	pcolor := styles.ProjectColorOrDefault(item.ProjectColor)

	switch item.Kind {
	case KindProject:
		arrow := "▶"
		if !item.Collapsed {
			arrow = "▼"
		}
		numHint := ""
		if item.ProjectNum >= 1 && item.ProjectNum <= 9 {
			numHint = fmt.Sprintf("[%d] ", item.ProjectNum)
		}
		prefix = arrow + " "
		label = numHint + item.Label
		if selected {
			return styles.ProjectSelectedStyle.
				Foreground(lipgloss.Color(pcolor)).
				Width(width).Render(indent + prefix + label)
		}
		return styles.ProjectStyle.
			Foreground(lipgloss.Color(pcolor)).
			Width(width).Render(indent + prefix + label)

	case KindTeam:
		arrow := "▶"
		if !item.Collapsed {
			arrow = "▼"
		}
		teamStatus := styles.StatusDot(item.Status)
		prefix = arrow + " "
		label = fmt.Sprintf("[team] %s %s", item.Label, teamStatus)
		st := styles.TeamStyle
		if selected {
			st = st.Background(styles.ColorSelected)
		}
		bar := styles.ProjectColorBar(pcolor)
		return bar + st.Width(width-1).Render(indent + prefix + label)

	case KindSession:
		// Bell badge replaces the status pip when a bell is pending. The badge
		// is toggled on/off by bellBlinkOn (driven by a tea.Tick in the Model)
		// to create a software blink effect independent of terminal ANSI blink.
		dot := styles.StatusDot(item.Status)
		if s.bellPending[item.SessionID] && s.bellBlinkOn {
			dot = styles.BellBadge()
		}
		badge := styles.AgentBadge(item.AgentType)
		rolePrefix := ""
		if item.TeamRole == string(state.RoleOrchestrator) {
			rolePrefix = styles.OrchestratorStyle.Render("★ ")
		}
		worktreeBadge := ""
		if item.IsWorktree && item.WorktreeBranch != "" {
			if item.WorktreeBranch != item.Label {
				worktreeBadge = " " + styles.WorktreeBadgeStyle.Render("⎇ "+item.WorktreeBranch)
			} else {
				worktreeBadge = " " + styles.WorktreeBadgeStyle.Render("⎇")
			}
		}
		// When the session has its own color, render the title with a
		// gradient foreground (project → session color) so it's always
		// visible, even when selected.
		titlePart := item.Label
		if item.SessionColor != "" && item.SessionColor != pcolor {
			if selected {
				titlePart = styles.GradientFg(item.Label, pcolor, item.SessionColor, styles.ColorSelected, true, false)
			} else {
				titlePart = styles.GradientFg(item.Label, pcolor, item.SessionColor, lipgloss.Color(""), false, false)
			}
		}
		label = fmt.Sprintf("%s%s %s %s%s", rolePrefix, dot, titlePart, badge, worktreeBadge)
		if active {
			label += " ←"
		}
		bar := styles.ProjectColorBar(pcolor)
		if item.SessionColor != "" && item.SessionColor != pcolor {
			// Title already has gradient styling; render rest with base style.
			if selected {
				return bar + styles.SessionSelectedStyle.Width(width-1).Render(indent + label)
			}
			return bar + styles.SessionStyle.Width(width-1).Render(indent + label)
		}
		if selected {
			return bar + styles.SessionSelectedStyle.Width(width-1).Render(indent + label)
		}
		return bar + styles.SessionStyle.Width(width-1).Render(indent + label)
	}
	return ""
}

// matchesSessionFilter checks if a session matches the filter query by title or agent type.
func matchesSessionFilter(sess *state.Session, query string) bool {
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(sess.Title), q) ||
		strings.Contains(strings.ToLower(string(sess.AgentType)), q)
}

func projectMatchesFilter(p *state.Project, query string) bool {
	q := strings.ToLower(query)
	if strings.Contains(strings.ToLower(p.Name), q) {
		return true
	}
	for _, sess := range p.Sessions {
		if matchesSessionFilter(sess, q) {
			return true
		}
	}
	for _, t := range p.Teams {
		if strings.Contains(strings.ToLower(t.Name), q) {
			return true
		}
		for _, sess := range t.Sessions {
			if matchesSessionFilter(sess, q) {
				return true
			}
		}
	}
	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
