package main

import (
	"bytes"
	"os"
	"path/filepath"
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

	if got, err := a.ListCustomAgents(); err != nil || len(got) != 0 {
		t.Fatalf("ListCustomAgents = %v, %v on a fresh state dir, want empty and no error", got, err)
	}

	in := []CustomAgent{
		{Name: "One", Cmd: []string{"one", "--a"}, Color: "#111111"},
		{Name: "Two", Cmd: []string{"two"}, Color: "#222222"},
	}
	if err := a.SaveCustomAgents(in); err != nil {
		t.Fatalf("SaveCustomAgents: %v", err)
	}

	out, err := a.ListCustomAgents()
	if err != nil {
		t.Fatalf("ListCustomAgents: %v", err)
	}
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
	if got, _ := a.ListCustomAgents(); len(got) != 0 {
		t.Error("a rejected save still wrote to disk")
	}
}

// TestListCustomAgentsSurfacesMalformedFile covers the binding the
// settings modal calls. A malformed agents.json must reject the Wails
// promise, not resolve empty — resolving empty renders as "no custom
// agents yet" and the next Save would overwrite the broken file,
// destroying every definition the user was about to repair.
func TestListCustomAgentsSurfacesMalformedFile(t *testing.T) {
	dir := isolateState(t)
	a := NewApp(t.TempDir())

	path := filepath.Join(dir, agent.CustomFileName)
	body := []byte(`[{"id":"one","name":"One","cmd":["one"],,}]`)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatalf("write %s: %v", agent.CustomFileName, err)
	}

	got, err := a.ListCustomAgents()
	if err == nil {
		t.Fatal("ListCustomAgents = nil error on a corrupt agents.json, want it surfaced")
	}
	if len(got) != 0 {
		t.Errorf("ListCustomAgents = %v, want no entries alongside the error", got)
	}

	// The corrupt file must still be on disk byte-for-byte: nothing in
	// the read path may destroy what the user has to hand-edit.
	after, readErr := os.ReadFile(path)
	if readErr != nil {
		t.Fatalf("read back %s: %v", agent.CustomFileName, readErr)
	}
	if !bytes.Equal(after, body) {
		t.Errorf("agents.json = %q after a failed load, want it untouched", after)
	}
}
