package main

import (
	"slices"
	"testing"

	"github.com/lucascaro/hive/internal/agent"
)

// isolateState points the app (and the agent catalog it configures)
// at a temp state dir, so these tests never read or write the user's
// real agents.json.
func isolateState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HIVE_STATE_DIR", dir)
	t.Cleanup(func() { agent.SetCustomDir("") })
	return dir
}

// TestNewAppConfiguresCustomAgentDir covers the wiring in NewApp: if
// SetCustomDir is not called there, ListAgents silently returns only
// built-ins and the launcher never shows a custom agent.
func TestNewAppConfiguresCustomAgentDir(t *testing.T) {
	isolateState(t)
	a := NewApp(t.TempDir())

	if err := a.SaveCustomAgents([]CustomAgent{
		{Name: "Claude Lite", Cmd: []string{"claude", "--model", "haiku"}, Color: "#8b5cf6"},
	}); err != nil {
		t.Fatalf("SaveCustomAgents: %v", err)
	}

	idx := slices.IndexFunc(a.ListAgents(), func(ai AgentInfo) bool { return ai.ID == "claude-lite" })
	if idx < 0 {
		t.Fatal("ListAgents omitted the custom agent")
	}
	got := a.ListAgents()[idx]
	if got.Name != "Claude Lite" || got.Color != "#8b5cf6" {
		t.Errorf("AgentInfo = %+v, want the saved name and color", got)
	}
	// Built-ins must still be there, and listed first.
	all := a.ListAgents()
	if !slices.ContainsFunc(all, func(ai AgentInfo) bool { return ai.ID == "claude" }) {
		t.Error("ListAgents dropped the built-ins")
	}
	if all[len(all)-1].ID != "claude-lite" {
		t.Errorf("custom agent at index %d, want last", idx)
	}
}

func TestCustomAgentsRoundTripThroughBindings(t *testing.T) {
	isolateState(t)
	a := NewApp(t.TempDir())

	if got := a.ListCustomAgents(); len(got) != 0 {
		t.Fatalf("ListCustomAgents = %v on a fresh state dir, want empty", got)
	}

	in := []CustomAgent{
		{Name: "One", Cmd: []string{"one", "--a"}, Color: "#111111"},
		{Name: "Two", Cmd: []string{"two"}, Color: "#222222"},
	}
	if err := a.SaveCustomAgents(in); err != nil {
		t.Fatalf("SaveCustomAgents: %v", err)
	}

	out := a.ListCustomAgents()
	if len(out) != 2 {
		t.Fatalf("ListCustomAgents = %d entries, want 2", len(out))
	}
	if out[0].ID != "one" || out[1].ID != "two" {
		t.Errorf("ids = %q/%q, want slugs assigned by Go", out[0].ID, out[1].ID)
	}
	if !slices.Equal(out[0].Cmd, []string{"one", "--a"}) {
		t.Errorf("cmd = %v, want the argv preserved", out[0].Cmd)
	}
}

// TestSaveCustomAgentsSurfacesValidationError is what makes a bad
// entry visible in the settings modal instead of vanishing into
// hived.log.
func TestSaveCustomAgentsSurfacesValidationError(t *testing.T) {
	isolateState(t)
	a := NewApp(t.TempDir())

	err := a.SaveCustomAgents([]CustomAgent{
		{ID: "claude", Name: "Hijack", Cmd: []string{"evil"}},
	})
	if err == nil {
		t.Fatal("SaveCustomAgents = nil for a built-in collision, want an error")
	}
	if len(a.ListCustomAgents()) != 0 {
		t.Error("a rejected save still wrote to disk")
	}
}
