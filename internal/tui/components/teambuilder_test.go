package components

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/state"
)

func TestTeamBuilder_StartActivates(t *testing.T) {
	tb := NewTeamBuilder()
	if tb.Active {
		t.Error("expected Active=false before Start")
	}
	tb.Start("/tmp/work")
	if !tb.Active {
		t.Error("expected Active=true after Start")
	}
	if tb.step != stepName {
		t.Errorf("expected step=stepName, got %d", tb.step)
	}
}

func TestTeamBuilder_HideDeactivates(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")
	tb.Hide()
	if tb.Active {
		t.Error("expected Active=false after Hide")
	}
}

func TestTeamBuilder_EmptyNameRejected(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")
	tb.input.SetValue("")

	cmd := tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tb.step != stepName {
		t.Errorf("empty name should not advance: step=%d", tb.step)
	}
	if cmd != nil {
		t.Error("expected nil cmd for empty name")
	}
}

func TestTeamBuilder_NameAdvancesToGoal(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")
	tb.input.SetValue("my-team")

	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tb.step != stepGoal {
		t.Errorf("expected step=stepGoal, got %d", tb.step)
	}
	if tb.spec.Name != "my-team" {
		t.Errorf("expected spec.Name=my-team, got %s", tb.spec.Name)
	}
}

func TestTeamBuilder_GoalAdvancesToOrchestrator(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")

	// Step 1: name
	tb.input.SetValue("team1")
	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Step 2: goal (empty is ok)
	tb.input.SetValue("do stuff")
	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if tb.step != stepOrchestrator {
		t.Errorf("expected step=stepOrchestrator, got %d", tb.step)
	}
	if tb.spec.Goal != "do stuff" {
		t.Errorf("expected spec.Goal=do stuff, got %s", tb.spec.Goal)
	}
	if !tb.agentPicker.Active {
		t.Error("expected agent picker to be active for orchestrator selection")
	}
}

func TestTeamBuilder_OrchestratorPickAdvancesToWorkerCount(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")

	// Advance through name and goal
	tb.input.SetValue("team1")
	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tb.input.SetValue("")
	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Simulate orchestrator pick (agent picker must be deactivated first,
	// as it would be after the real picker sends the message)
	tb.agentPicker.Hide()
	tb.Update(AgentPickedMsg{AgentType: state.AgentGemini})

	if tb.step != stepWorkerCount {
		t.Errorf("expected step=stepWorkerCount, got %d", tb.step)
	}
	if tb.spec.OrchestratorAgent != state.AgentGemini {
		t.Errorf("expected orchestrator=gemini, got %s", tb.spec.OrchestratorAgent)
	}
}

func TestTeamBuilder_WorkerCountParsing(t *testing.T) {
	tests := []struct {
		input string
		want  int
	}{
		{"abc", 2}, // invalid → default 2
		{"0", 2},   // too low → default 2
		{"11", 2},  // too high → default 2
		{"5", 5},
		{"1", 1},
		{"10", 10},
	}

	for _, tt := range tests {
		tb := NewTeamBuilder()
		tb.Start("/tmp/work")
		tb.step = stepWorkerCount
		tb.workerCount = 2
		tb.input.SetValue(tt.input)

		tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
		if tb.workerCount != tt.want {
			t.Errorf("input=%q: workerCount=%d, want %d", tt.input, tb.workerCount, tt.want)
		}
	}
}

func TestTeamBuilder_EscAtAnyStepHides(t *testing.T) {
	steps := []wizardStep{stepName, stepGoal, stepWorkerCount, stepWorkDir, stepConfirm}

	for _, step := range steps {
		tb := NewTeamBuilder()
		tb.Start("/tmp/work")
		tb.step = step

		cmd := tb.Update(tea.KeyMsg{Type: tea.KeyEscape})
		if tb.Active {
			t.Errorf("step=%d: expected Active=false after esc", step)
		}
		if cmd == nil {
			t.Errorf("step=%d: expected CancelledMsg cmd", step)
			continue
		}
		msg := cmd()
		if _, ok := msg.(CancelledMsg); !ok {
			t.Errorf("step=%d: expected CancelledMsg, got %T", step, msg)
		}
	}
}

func TestTeamBuilder_WorkDirAdvancesToConfirm(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")
	tb.step = stepWorkDir
	tb.input.SetValue("/custom/dir")

	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tb.step != stepConfirm {
		t.Errorf("expected step=stepConfirm, got %d", tb.step)
	}
	if tb.spec.SharedWorkDir != "/custom/dir" {
		t.Errorf("expected SharedWorkDir=/custom/dir, got %s", tb.spec.SharedWorkDir)
	}
}

func TestTeamBuilder_ConfirmEmitsTeamBuiltMsg(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")
	tb.step = stepConfirm
	tb.spec.Name = "test-team"
	tb.spec.OrchestratorAgent = state.AgentClaude
	tb.workerAgents = []state.AgentType{state.AgentCodex}

	cmd := tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if tb.Active {
		t.Error("expected Active=false after confirm")
	}
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	msg := cmd()
	built, ok := msg.(TeamBuiltMsg)
	if !ok {
		t.Fatalf("expected TeamBuiltMsg, got %T", msg)
	}
	if built.Spec.Name != "test-team" {
		t.Errorf("expected name=test-team, got %s", built.Spec.Name)
	}
	if len(built.Spec.Workers) != 1 || built.Spec.Workers[0] != state.AgentCodex {
		t.Errorf("expected workers=[codex], got %v", built.Spec.Workers)
	}
}

func TestTeamBuilder_WorkerPickingLoop(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")

	// Advance to worker count step
	tb.step = stepWorkerCount
	tb.workerCount = 2
	tb.input.SetValue("3")
	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})

	// Should have 3 worker slots and picker showing for worker 0
	if tb.workerCount != 3 {
		t.Fatalf("expected workerCount=3, got %d", tb.workerCount)
	}
	if tb.pickerStep != "worker0" {
		t.Errorf("expected pickerStep=worker0, got %s", tb.pickerStep)
	}

	// Pick worker 0 → should auto-advance to worker 1
	tb.agentPicker.Hide()
	tb.Update(AgentPickedMsg{AgentType: state.AgentCodex})
	if tb.workerIdx != 1 {
		t.Errorf("expected workerIdx=1, got %d", tb.workerIdx)
	}
	if tb.workerAgents[0] != state.AgentCodex {
		t.Errorf("worker 0: expected codex, got %s", tb.workerAgents[0])
	}
	if tb.pickerStep != "worker1" {
		t.Errorf("expected pickerStep=worker1, got %s", tb.pickerStep)
	}

	// Pick worker 1 → should auto-advance to worker 2
	tb.agentPicker.Hide()
	tb.Update(AgentPickedMsg{AgentType: state.AgentGemini})
	if tb.workerIdx != 2 {
		t.Errorf("expected workerIdx=2, got %d", tb.workerIdx)
	}
	if tb.workerAgents[1] != state.AgentGemini {
		t.Errorf("worker 1: expected gemini, got %s", tb.workerAgents[1])
	}

	// Pick worker 2 (last) → should advance to stepWorkDir
	tb.agentPicker.Hide()
	tb.Update(AgentPickedMsg{AgentType: state.AgentAider})
	if tb.step != stepWorkDir {
		t.Errorf("expected step=stepWorkDir after last worker, got %d", tb.step)
	}
	if tb.workerAgents[2] != state.AgentAider {
		t.Errorf("worker 2: expected aider, got %s", tb.workerAgents[2])
	}
}

func TestTeamBuilder_CancelFromOrchestratorStep(t *testing.T) {
	tb := NewTeamBuilder()
	tb.Start("/tmp/work")

	// Advance to orchestrator step
	tb.input.SetValue("team1")
	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	tb.input.SetValue("")
	tb.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if tb.step != stepOrchestrator {
		t.Fatalf("precondition: expected step=stepOrchestrator, got %d", tb.step)
	}

	// Simulate cancel from agent picker
	tb.agentPicker.Hide()
	cmd := tb.Update(CancelledMsg{})
	if tb.Active {
		t.Error("expected Active=false after CancelledMsg")
	}
	if cmd != nil {
		t.Error("expected nil cmd from CancelledMsg handler")
	}
}

func TestTeamBuilder_InactiveUpdateIsNoop(t *testing.T) {
	tb := NewTeamBuilder()
	cmd := tb.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("expected nil cmd when inactive")
	}
}
