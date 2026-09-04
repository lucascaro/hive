//go:build darwin

package main

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
	"github.com/lucascaro/hive/internal/wire"
)

func welcome(contract int) wire.Welcome {
	return wire.Welcome{Release: "1.2.3", BuildID: "abc1234", DaemonContract: contract}
}

func TestBuildModelGroupsSessionsByProject(t *testing.T) {
	m := BuildModel(
		[]wire.ProjectInfo{
			{ID: "p2", Name: "second", Order: 1},
			{ID: "p1", Name: "first", Order: 0},
		},
		[]wire.SessionInfo{
			{ID: "b", Name: "beta", ProjectID: "p1", Order: 1, Alive: true},
			{ID: "a", Name: "alpha", ProjectID: "p1", Order: 0, Alive: true},
			{ID: "c", Name: "gamma", ProjectID: "p2", Order: 0, Alive: true},
		},
		welcome(buildinfo.DaemonContract), buildinfo.DaemonContract,
	)

	if len(m.Projects) != 2 {
		t.Fatalf("groups = %d, want 2", len(m.Projects))
	}
	// Projects in daemon order, sessions in daemon order within them —
	// the menu must not invent an ordering the sidebar disagrees with.
	if m.Projects[0].Name != "first" || m.Projects[1].Name != "second" {
		t.Errorf("project order = %q, %q", m.Projects[0].Name, m.Projects[1].Name)
	}
	if got := []string{m.Projects[0].Sessions[0].ID, m.Projects[0].Sessions[1].ID}; got[0] != "a" || got[1] != "b" {
		t.Errorf("session order = %v, want [a b]", got)
	}
	if m.Sessions != 3 {
		t.Errorf("Sessions = %d, want 3", m.Sessions)
	}
}

// A session whose project the menu does not know about is still
// something the user can click. Dropping it would make the list
// disagree with the count printed right above it.
func TestBuildModelKeepsSessionsWithUnknownProject(t *testing.T) {
	m := BuildModel(
		[]wire.ProjectInfo{{ID: "p1", Name: "known"}},
		[]wire.SessionInfo{
			{ID: "a", Name: "alpha", ProjectID: "p1", Alive: true},
			{ID: "orphan", Name: "orphan", ProjectID: "gone", Alive: true},
			{ID: "legacy", Name: "legacy", Alive: true},
		},
		welcome(buildinfo.DaemonContract), buildinfo.DaemonContract,
	)

	var listed int
	for _, p := range m.Projects {
		listed += len(p.Sessions)
	}
	if listed != m.Sessions {
		t.Errorf("listed %d sessions but the summary counts %d", listed, m.Sessions)
	}
}

// An empty project is a row that can never be clicked.
func TestBuildModelDropsEmptyProjects(t *testing.T) {
	m := BuildModel(
		[]wire.ProjectInfo{{ID: "p1", Name: "busy"}, {ID: "p2", Name: "idle"}},
		[]wire.SessionInfo{{ID: "a", ProjectID: "p1", Alive: true}},
		welcome(buildinfo.DaemonContract), buildinfo.DaemonContract,
	)
	if len(m.Projects) != 1 || m.Projects[0].Name != "busy" {
		t.Errorf("groups = %+v, want only \"busy\"", m.Projects)
	}
}

func TestBuildModelCountsAttention(t *testing.T) {
	m := BuildModel(
		[]wire.ProjectInfo{{ID: "p1", Name: "p"}},
		[]wire.SessionInfo{
			{ID: "a", ProjectID: "p1", Alive: true, NeedsAttention: true},
			{ID: "b", ProjectID: "p1", Alive: true},
			{ID: "c", ProjectID: "p1", Alive: true, NeedsAttention: true},
		},
		welcome(buildinfo.DaemonContract), buildinfo.DaemonContract,
	)
	if m.Attention != 2 {
		t.Errorf("Attention = %d, want 2", m.Attention)
	}
	if !strings.Contains(m.SummaryLine(), "2 need you") {
		t.Errorf("SummaryLine = %q", m.SummaryLine())
	}
}

func TestSummaryLinePluralisation(t *testing.T) {
	one := BuildModel(
		[]wire.ProjectInfo{{ID: "p1", Name: "p"}},
		[]wire.SessionInfo{{ID: "a", ProjectID: "p1", Alive: true, NeedsAttention: true}},
		welcome(buildinfo.DaemonContract), buildinfo.DaemonContract,
	)
	got := one.SummaryLine()
	for _, want := range []string{"1 session", "1 project", "1 needs you"} {
		if !strings.Contains(got, want) {
			t.Errorf("SummaryLine = %q, missing %q", got, want)
		}
	}
}

func TestSummaryLineNoSessions(t *testing.T) {
	m := BuildModel(nil, nil, welcome(buildinfo.DaemonContract), buildinfo.DaemonContract)
	if m.SummaryLine() != "No sessions" {
		t.Errorf("SummaryLine = %q", m.SummaryLine())
	}
}

// The disconnected model must say so rather than render an empty
// session list, which reads as "the daemon is up and you have nothing
// running" — the opposite of the truth.
func TestDisconnectedModelSaysSo(t *testing.T) {
	m := Disconnected()
	if m.Connected {
		t.Fatal("Disconnected().Connected must be false")
	}
	if !strings.Contains(m.HeaderLine(), "not running") {
		t.Errorf("HeaderLine = %q", m.HeaderLine())
	}
	if _, show := m.ContractLine(); show {
		t.Error("a disconnected daemon has no contract to compare")
	}
}

func TestContractLine(t *testing.T) {
	ours := buildinfo.DaemonContract

	if _, show := BuildModel(nil, nil, welcome(ours), ours).ContractLine(); show {
		t.Error("a matching contract needs no line of its own")
	}

	line, show := BuildModel(nil, nil, welcome(ours+1), ours).ContractLine()
	if !show || !strings.Contains(line, "restart") {
		t.Errorf("mismatch line = %q (shown=%v); it must name the remedy", line, show)
	}

	line, show = BuildModel(nil, nil, welcome(0), ours).ContractLine()
	if !show || !strings.Contains(line, "predates") {
		t.Errorf("unknown-contract line = %q (shown=%v)", line, show)
	}
}

func TestSessionRowLabel(t *testing.T) {
	cases := []struct {
		name    string
		row     SessionRow
		project string
		want    string
	}{
		{"plain", SessionRow{Name: "alpha", Alive: true}, "", "  alpha"},
		{"project named", SessionRow{Name: "alpha", Alive: true}, "hive", "  hive · alpha"},
		{"attention marker leads", SessionRow{Name: "alpha", Alive: true, NeedsAttention: true}, "hive", "● hive · alpha"},
		{"dead session", SessionRow{Name: "alpha"}, "", "  alpha (stopped)"},
		{"title appended", SessionRow{Name: "alpha", Title: "npm test", Alive: true}, "", "  alpha — npm test"},
		// A title identical to the name is noise, not information.
		{"redundant title dropped", SessionRow{Name: "alpha", Title: "alpha", Alive: true}, "", "  alpha"},
		// A dead session's title is stale by definition, so the
		// stopped marker wins.
		{"dead outranks title", SessionRow{Name: "alpha", Title: "npm test"}, "", "  alpha (stopped)"},
		{"nameless falls back to id", SessionRow{ID: "abc123", Alive: true}, "", "  abc123"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.row.LabelIn(c.project); got != c.want {
				t.Errorf("Label() = %q, want %q", got, c.want)
			}
		})
	}
}

func TestHeaderLineDegradesGracefully(t *testing.T) {
	// A daemon built without --version reports "dev"; one built before
	// the Release field reports nothing at all. Neither may render as
	// a hole in the string.
	m := BuildModel(nil, nil, wire.Welcome{DaemonContract: 1}, 1)
	got := m.HeaderLine()
	if strings.Contains(got, "()") || strings.Contains(got, "  ") {
		t.Errorf("HeaderLine = %q; missing fields must be named, not blank", got)
	}
}

// main starts the client goroutine before systray.Run, and the daemon
// sends its snapshot on handshake — so the first Model routinely
// arrives before onReady has created a single menu item. Touching
// systray then is undefined, and dropping the Model instead would
// leave the menu empty until the next daemon event, which on a quiet
// machine never comes.
//
// Only the not-ready path is exercised here: the ready path calls into
// systray, which needs a real status bar and cannot run under `go
// test`. That is exactly why the decision this gate makes lives in a
// plain bool rather than inside the systray call site.
func TestMenuHoldsUpdatesUntilReady(t *testing.T) {
	m := NewMenu()

	early := BuildModel(
		[]wire.ProjectInfo{{ID: "p1", Name: "early"}},
		[]wire.SessionInfo{{ID: "a", ProjectID: "p1", Alive: true}},
		welcome(buildinfo.DaemonContract), buildinfo.DaemonContract,
	)
	m.Update(early) // must not reach systray

	if m.pending == nil {
		t.Fatal("a pre-ready Model was dropped; the menu would stay empty")
	}
	if m.header != nil {
		t.Error("a pre-ready Update touched systray")
	}

	// Only the newest is worth keeping — the older one is already stale.
	later := BuildModel(nil, nil, welcome(buildinfo.DaemonContract), buildinfo.DaemonContract)
	m.Update(later)
	if m.pending.Sessions != later.Sessions {
		t.Errorf("held Model has %d sessions, want the newest (%d)",
			m.pending.Sessions, later.Sessions)
	}
}

// The confirm dialog guards "this ends every running shell and agent",
// so its text is built by string interpolation into AppleScript — and
// session names, which come from the user's shell, reach these
// dialogs. Quoting is the boundary.
func TestAsAppleScriptStringQuotes(t *testing.T) {
	cases := []struct{ in, want string }{
		{`plain`, `"plain"`},
		{`say "hi"`, `"say \"hi\""`},
		{`back\slash`, `"back\\slash"`},
		{"two\nlines", `"two\nlines"`},
		// The shape that would otherwise close the string and run
		// whatever follows.
		{`" & (do shell script "id") & "`, `"\" & (do shell script \"id\") & \""`},
	}
	for _, c := range cases {
		if got := asAppleScriptString(c.in); got != c.want {
			t.Errorf("asAppleScriptString(%q) = %s, want %s", c.in, got, c.want)
		}
	}
}
