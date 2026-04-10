package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/state"
)

func TestRecoveryPicker_NewEmptySessions(t *testing.T) {
	rp := NewRecoveryPicker(nil)
	if rp.Active {
		t.Error("expected Active=false for empty sessions")
	}
}

func TestRecoveryPicker_NewWithSessions(t *testing.T) {
	sessions := []state.RecoverableSession{
		{TmuxSession: "hive-aaa", WindowIndex: 0, DetectedAgentType: state.AgentClaude},
		{TmuxSession: "hive-bbb", WindowIndex: 1, DetectedAgentType: state.AgentCodex},
	}
	rp := NewRecoveryPicker(sessions)
	if !rp.Active {
		t.Error("expected Active=true")
	}
	if rp.cursor != 0 {
		t.Errorf("expected cursor=0, got %d", rp.cursor)
	}
}

func TestRecoveryPicker_CursorNavigation(t *testing.T) {
	sessions := []state.RecoverableSession{
		{TmuxSession: "a"}, {TmuxSession: "b"}, {TmuxSession: "c"},
	}
	rp := NewRecoveryPicker(sessions)

	rp, _ = rp.Update(keyPress("j"))
	if rp.cursor != 1 {
		t.Errorf("after j: cursor=%d, want 1", rp.cursor)
	}
	rp, _ = rp.Update(keyType(tea.KeyDown))
	if rp.cursor != 2 {
		t.Errorf("after down: cursor=%d, want 2", rp.cursor)
	}
	// Clamp at bottom
	rp, _ = rp.Update(keyPress("j"))
	if rp.cursor != 2 {
		t.Errorf("clamp bottom: cursor=%d, want 2", rp.cursor)
	}

	rp, _ = rp.Update(keyPress("k"))
	if rp.cursor != 1 {
		t.Errorf("after k: cursor=%d, want 1", rp.cursor)
	}
	rp, _ = rp.Update(keyType(tea.KeyUp))
	if rp.cursor != 0 {
		t.Errorf("after up: cursor=%d, want 0", rp.cursor)
	}
	// Clamp at top
	rp, _ = rp.Update(keyPress("k"))
	if rp.cursor != 0 {
		t.Errorf("clamp top: cursor=%d, want 0", rp.cursor)
	}
}

func TestRecoveryPicker_SpaceToggle(t *testing.T) {
	sessions := []state.RecoverableSession{{TmuxSession: "a"}, {TmuxSession: "b"}}
	rp := NewRecoveryPicker(sessions)

	rp, _ = rp.Update(keyPress(" "))
	if !rp.selected[0] {
		t.Error("expected selected[0]=true")
	}
	rp, _ = rp.Update(keyPress(" "))
	if rp.selected[0] {
		t.Error("expected selected[0]=false")
	}
}

func TestRecoveryPicker_ToggleAll(t *testing.T) {
	sessions := []state.RecoverableSession{{TmuxSession: "a"}, {TmuxSession: "b"}, {TmuxSession: "c"}}
	rp := NewRecoveryPicker(sessions)

	// None → all
	rp, _ = rp.Update(keyPress("a"))
	for i, s := range rp.selected {
		if !s {
			t.Errorf("none→all: selected[%d]=false", i)
		}
	}
	// All → none
	rp, _ = rp.Update(keyPress("a"))
	for i, s := range rp.selected {
		if s {
			t.Errorf("all→none: selected[%d]=true", i)
		}
	}
}

func TestRecoveryPicker_AgentTypeCycleRight(t *testing.T) {
	sessions := []state.RecoverableSession{
		{TmuxSession: "a", DetectedAgentType: state.AgentClaude},
	}
	rp := NewRecoveryPicker(sessions)

	// Claude is index 0, right should move to Codex (index 1)
	rp, _ = rp.Update(keyPress("l"))
	if rp.sessions[0].DetectedAgentType != state.AgentCodex {
		t.Errorf("after right: agent=%s, want codex", rp.sessions[0].DetectedAgentType)
	}
}

func TestRecoveryPicker_AgentTypeCycleLeft(t *testing.T) {
	sessions := []state.RecoverableSession{
		{TmuxSession: "a", DetectedAgentType: state.AgentClaude},
	}
	rp := NewRecoveryPicker(sessions)

	// Claude is index 0, left should wrap to Custom (last)
	rp, _ = rp.Update(keyPress("h"))
	if rp.sessions[0].DetectedAgentType != state.AgentCustom {
		t.Errorf("after left from claude: agent=%s, want custom", rp.sessions[0].DetectedAgentType)
	}
}

func TestRecoveryPicker_AgentTypeCycleWrapRight(t *testing.T) {
	sessions := []state.RecoverableSession{
		{TmuxSession: "a", DetectedAgentType: state.AgentCustom},
	}
	rp := NewRecoveryPicker(sessions)

	// Custom is last, right should wrap to Claude (first)
	rp, _ = rp.Update(keyType(tea.KeyRight))
	if rp.sessions[0].DetectedAgentType != state.AgentClaude {
		t.Errorf("after right from custom: agent=%s, want claude", rp.sessions[0].DetectedAgentType)
	}
}

func TestAgentTypeIndex_KnownTypes(t *testing.T) {
	tests := []struct {
		agent state.AgentType
		want  int
	}{
		{state.AgentClaude, 0},
		{state.AgentCodex, 1},
		{state.AgentGemini, 2},
		{state.AgentCustom, len(allAgentTypes) - 1},
	}
	for _, tt := range tests {
		got := agentTypeIndex(tt.agent)
		if got != tt.want {
			t.Errorf("agentTypeIndex(%s)=%d, want %d", tt.agent, got, tt.want)
		}
	}
}

func TestAgentTypeIndex_UnknownDefaultsToCustom(t *testing.T) {
	got := agentTypeIndex("unknown-agent")
	want := len(allAgentTypes) - 1
	if got != want {
		t.Errorf("agentTypeIndex(unknown)=%d, want %d", got, want)
	}
}

func TestRecoveryPicker_EnterReturnsSelected(t *testing.T) {
	sessions := []state.RecoverableSession{
		{TmuxSession: "a", WindowIndex: 0, DetectedAgentType: state.AgentClaude},
		{TmuxSession: "b", WindowIndex: 1, DetectedAgentType: state.AgentCodex},
		{TmuxSession: "c", WindowIndex: 2, DetectedAgentType: state.AgentGemini},
	}
	rp := NewRecoveryPicker(sessions)
	rp.selected[0] = true
	rp.selected[2] = true

	rp, cmd := rp.Update(keyType(tea.KeyEnter))
	if rp.Active {
		t.Error("expected Active=false")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	done, ok := msg.(RecoveryPickerDoneMsg)
	if !ok {
		t.Fatalf("expected RecoveryPickerDoneMsg, got %T", msg)
	}
	if len(done.Selected) != 2 {
		t.Fatalf("expected 2 selected, got %d", len(done.Selected))
	}
	if done.Selected[0].TmuxSession != "a" || done.Selected[1].TmuxSession != "c" {
		t.Errorf("unexpected selected sessions: %+v", done.Selected)
	}
}

func TestRecoveryPicker_EnterReturnsModifiedAgentType(t *testing.T) {
	sessions := []state.RecoverableSession{
		{TmuxSession: "a", DetectedAgentType: state.AgentClaude},
	}
	rp := NewRecoveryPicker(sessions)
	rp.selected[0] = true

	// Change agent type to codex
	rp, _ = rp.Update(keyPress("l"))
	rp, cmd := rp.Update(keyType(tea.KeyEnter))
	msg := cmd()
	done := msg.(RecoveryPickerDoneMsg)
	if done.Selected[0].DetectedAgentType != state.AgentCodex {
		t.Errorf("expected codex, got %s", done.Selected[0].DetectedAgentType)
	}
}

func TestRecoveryPicker_EscReturnsNil(t *testing.T) {
	sessions := []state.RecoverableSession{{TmuxSession: "a"}}
	rp := NewRecoveryPicker(sessions)
	rp.selected[0] = true

	rp, cmd := rp.Update(keyType(tea.KeyEscape))
	if rp.Active {
		t.Error("expected Active=false")
	}
	msg := cmd()
	done := msg.(RecoveryPickerDoneMsg)
	if done.Selected != nil {
		t.Errorf("expected nil Selected on esc, got %v", done.Selected)
	}
}
