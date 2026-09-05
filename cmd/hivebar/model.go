//go:build darwin

package main

import (
	"fmt"
	"sort"

	"github.com/lucascaro/hive/internal/wire"
)

// Model is everything the menu renders, derived from one daemon
// snapshot. It is pure data with no systray types in it, which is the
// point: a status-bar item cannot be asserted on in a test, so all the
// decisions live here and menu.go is left with nothing but the
// translation into menu items.
type Model struct {
	// Connected is false when the daemon is unreachable. Every other
	// field is then meaningless and the menu says so instead of
	// rendering an empty session list, which reads as "no sessions".
	Connected bool

	DaemonRelease  string
	DaemonBuild    string
	DaemonContract int

	// Compatibility of the hivebar binary itself with the daemon it is
	// talking to. Reported because hivebar ships inside the same bundle
	// as the GUI, so a mismatch here means the user is looking at a
	// menu bar from a different install than the daemon they are
	// running.
	ContractMatches bool

	Projects []ProjectGroup

	Sessions int
	// Waiting counts the sessions actually blocked on the user —
	// state ∈ {waiting_input, waiting_permission} — rather than the
	// sessions whose bell happens to be unacknowledged.
	Waiting int
}

// ProjectGroup is one project and the sessions under it, in the order
// the daemon reports.
type ProjectGroup struct {
	ID       string
	Name     string
	Sessions []SessionRow
}

// SessionRow is one clickable session line.
type SessionRow struct {
	ID    string
	Name  string
	Title string
	// Waiting drives the marker on the row. Rendered rather than
	// counted-only because "3 waiting on you" without saying which is a
	// prompt to open the GUI and hunt. It is the same predicate the
	// count uses, so the marker and the summary can never disagree.
	Waiting bool
	Alive   bool
}

// stateContract is the DaemonContract generation that introduced
// SessionInfo.state (internal/buildinfo/contract.go, history entry 3).
//
// It exists because `state` cannot answer "does this daemon report
// state at all": StateIdle is the empty string, so an idle session on a
// current daemon and every session on an old one look identical on the
// wire. The daemon's own contract number is the only honest
// discriminator, and Welcome already carries it.
const stateContract = 3

// waiting reports whether a session is blocked on the user.
//
// Below stateContract the daemon sends no state, so needs_attention —
// the bell-driven flag hivebar read before this — is all there is, and
// falling back to it keeps an old daemon's menu working rather than
// showing a permanent zero.
func waiting(s wire.SessionInfo, daemonContract int) bool {
	if daemonContract < stateContract {
		return s.NeedsAttention
	}
	return s.State == wire.StateWaitingInput || s.State == wire.StateWaitingPermission
}

// BuildModel groups a snapshot into what the menu shows.
//
// Sessions whose project is unknown are collected under a synthetic
// group rather than dropped: an unassigned or legacy session is still
// something the user can click, and silently omitting it would make the
// menu disagree with the session count beside it.
func BuildModel(
	projects []wire.ProjectInfo,
	sessions []wire.SessionInfo,
	welcome wire.Welcome,
	ourContract int,
) Model {
	m := Model{
		Connected:       true,
		DaemonRelease:   welcome.Release,
		DaemonBuild:     welcome.BuildID,
		DaemonContract:  welcome.DaemonContract,
		ContractMatches: welcome.DaemonContract != 0 && welcome.DaemonContract == ourContract,
		Sessions:        len(sessions),
	}

	order := make(map[string]int, len(projects))
	byID := make(map[string]*ProjectGroup, len(projects))
	sorted := append([]wire.ProjectInfo(nil), projects...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Order < sorted[j].Order })
	for i, p := range sorted {
		g := &ProjectGroup{ID: p.ID, Name: p.Name}
		byID[p.ID] = g
		order[p.ID] = i
	}

	var loose *ProjectGroup
	sortedSessions := append([]wire.SessionInfo(nil), sessions...)
	sort.SliceStable(sortedSessions, func(i, j int) bool {
		return sortedSessions[i].Order < sortedSessions[j].Order
	})
	for _, s := range sortedSessions {
		w := waiting(s, welcome.DaemonContract)
		if w {
			m.Waiting++
		}
		row := SessionRow{
			ID:      s.ID,
			Name:    s.Name,
			Title:   s.Title,
			Waiting: w,
			Alive:   s.Alive,
		}
		g, ok := byID[s.ProjectID]
		if !ok {
			if loose == nil {
				loose = &ProjectGroup{ID: "", Name: "Other"}
			}
			g = loose
		}
		g.Sessions = append(g.Sessions, row)
	}

	for _, p := range sorted {
		g := byID[p.ID]
		// Projects with no sessions are dropped. The menu is a
		// status readout, not a project browser — an empty project is
		// a row that can never be clicked.
		if len(g.Sessions) == 0 {
			continue
		}
		m.Projects = append(m.Projects, *g)
	}
	if loose != nil {
		m.Projects = append(m.Projects, *loose)
	}
	return m
}

// Disconnected is the model shown when the daemon cannot be reached.
func Disconnected() Model { return Model{} }

// HeaderLine is the first line of the menu: what daemon is running.
func (m Model) HeaderLine() string {
	if !m.Connected {
		return "Hive daemon not running"
	}
	release := m.DaemonRelease
	if release == "" {
		release = "dev"
	}
	build := m.DaemonBuild
	if build == "" {
		build = "unknown build"
	}
	return fmt.Sprintf("hived %s (%s)", release, build)
}

// ContractLine reports the compatibility state, and is only worth
// showing when something is wrong — a matching contract is the
// expected case and does not need a line of its own.
func (m Model) ContractLine() (string, bool) {
	if !m.Connected || m.ContractMatches {
		return "", false
	}
	if m.DaemonContract == 0 {
		return "Daemon predates this menu bar — restart it", true
	}
	return fmt.Sprintf("Daemon contract %d, this build expects %d — restart the daemon",
		m.DaemonContract, buildContract()), true
}

// SummaryLine is the one-line count under the header.
func (m Model) SummaryLine() string {
	if !m.Connected {
		return "Open Hive to start it"
	}
	if m.Sessions == 0 {
		return "No sessions"
	}
	s := fmt.Sprintf("%s across %s",
		plural(m.Sessions, "session"), plural(len(m.Projects), "project"))
	if m.Waiting > 0 {
		s += fmt.Sprintf(" · %d waiting on you", m.Waiting)
	}
	return s
}

// LabelIn renders one session row, naming the project it belongs to.
//
// The project rides on the row instead of being a submenu the row sits
// inside: the menu is a fixed pool of items so that it never reorders
// itself (see menu.go), and a submenu tree cannot be a fixed pool
// without one pool per project. A row you can click from the top level
// also beats one you have to hover a parent to reach.
func (m SessionRow) LabelIn(project string) string {
	name := m.Name
	if name == "" {
		name = m.ID
	}
	prefix := "  "
	if m.Waiting {
		// A leading marker rather than a trailing one: menu items are
		// left-aligned and of wildly differing widths, so a trailing
		// glyph lands in a different place on every row and stops
		// reading as a column.
		prefix = "● "
	}
	label := prefix + name
	if project != "" {
		label = prefix + project + " · " + name
	}
	if !m.Alive {
		label += " (stopped)"
	} else if m.Title != "" && m.Title != name {
		label += " — " + m.Title
	}
	return label
}

func plural(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
