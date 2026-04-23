package components

import (
	"fmt"
	"log"
	"os"
	"strings"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/lucascaro/hive/internal/config"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

var (
	statusLog     = log.New(os.Stderr, "[status] ", log.Ltime)
	statusLogOnce sync.Once
)

// InitStatusLog upgrades the status logger from stderr to the hive log file.
// Called once from tui.New() so the log path is resolved after env overrides.
func InitStatusLog() {
	statusLogOnce.Do(func() {
		f, err := os.OpenFile(config.LogPath(), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return
		}
		statusLog = log.New(f, "[status] ", log.Ltime|log.Lmicroseconds)
	})
}

// StatusBar renders the two-line status bar at the bottom.
type StatusBar struct {
	Width   int
	Version string
}

// View renders the status bar given the current app state and pre-computed hints.
// hints is a fully-rendered hint line (from help.Model or a custom string for
// filter/confirm states); it is truncated to fit the available width.
func (sb *StatusBar) View(appState *state.AppState, hints string) string {
	w := sb.Width
	if w <= 0 {
		w = 80
	}
	// StatusBarStyle has Padding(0,1) — 1 char on each side.
	// Truncate content to the inner content area so it never wraps
	// and adds an extra line, which would make the frame taller than
	// the terminal and trigger an unwanted terminal scroll.
	// We subtract an extra column as a safety margin: some Unicode
	// symbols (status dots ●/◉, spinners ⟳, ellipsis …) are measured
	// as 1-wide by ansi.StringWidth but rendered as 2-wide in certain
	// terminals/fonts.  The 1-column margin prevents accidental line
	// wrap when that mismatch occurs.
	innerW := w - 3
	if innerW < 1 {
		innerW = 1
	}

	// Line 1: breadcrumb + status
	rawBreadcrumb := buildBreadcrumb(appState, sb.Version)
	breadcrumb := ansi.Truncate(rawBreadcrumb, innerW, "")
	line1 := styles.StatusBarStyle.Width(w).Render(breadcrumb)

	// Line 2: key hints (pre-computed by caller)
	truncHints := ansi.Truncate(hints, innerW, "")
	line2 := styles.StatusBarStyle.Width(w).Render(truncHints)

	joined := lipgloss.JoinVertical(lipgloss.Left, line1, line2)
	joinedLines := strings.Count(joined, "\n") + 1
	line1Lines := strings.Count(line1, "\n") + 1
	line2Lines := strings.Count(line2, "\n") + 1
	statusLog.Printf("View: w=%d innerW=%d breadcrumbRawW=%d hintsRawW=%d line1=%d line2=%d total=%d%s",
		w, innerW,
		ansi.StringWidth(rawBreadcrumb), ansi.StringWidth(hints),
		line1Lines, line2Lines, joinedLines,
		func() string {
			if joinedLines != 2 {
				return fmt.Sprintf(" HEIGHT_MISMATCH(want=2 got=%d)", joinedLines)
			}
			return ""
		}(),
	)
	return joined
}

func buildBreadcrumb(s *state.AppState, version string) string {
	dim := lipgloss.NewStyle().Faint(true)
	titlePart := styles.TitleStyle.Render("hive")
	if version != "" {
		titlePart += " " + dim.Render("v"+version)
	}
	parts := []string{titlePart}

	proj := s.ActiveProject()
	if proj != nil {
		parts = append(parts, proj.Name)
	}

	sess := s.ActiveSession()
	if sess != nil {
		if sess.TeamID != "" {
			// Find team name
			if proj != nil {
				for _, t := range proj.Teams {
					if t.ID == sess.TeamID {
						parts = append(parts, "[team] "+t.Name)
						break
					}
				}
			}
		}
		statusDot := styles.StatusDot(string(sess.Status))
		parts = append(parts, fmt.Sprintf("%s %s %s [%s]",
			sess.Title, styles.AgentBadge(string(sess.AgentType)), statusDot, sess.Status))
	}

	sep := " " + styles.BreadcrumbSeparatorStyle.Render("/") + " "
	if s.InstallingAgent != "" {
		spinner := lipgloss.NewStyle().Foreground(styles.ColorWarning).Render("⟳ Installing " + s.InstallingAgent + "…")
		return spinner + "  " + strings.Join(parts, sep)
	}
	if s.LastError != "" {
		return styles.ErrorStyle.Render("Error: "+s.LastError) + "  " + strings.Join(parts, sep)
	}
	return strings.Join(parts, sep)
}
