package components

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/lucascaro/hive/internal/state"
	"github.com/lucascaro/hive/internal/tui/styles"
)

// TeamSpec holds the configuration collected by the wizard.
type TeamSpec struct {
	Name              string
	Goal              string
	OrchestratorAgent state.AgentType
	Workers           []state.AgentType
	SharedWorkDir     string
}

// TeamBuiltMsg is sent when the wizard completes.
type TeamBuiltMsg struct {
	Spec TeamSpec
}

type wizardStep int

const (
	stepName wizardStep = iota
	stepGoal
	stepOrchestrator
	stepWorkerCount
	stepWorkDir
	stepConfirm
)

// TeamBuilder is a multi-step wizard for creating agent teams.
type TeamBuilder struct {
	Active  bool
	step    wizardStep
	spec    TeamSpec
	input   textinput.Model
	// For orchestrator/worker agent selection (uses AgentPicker inline)
	pickerStep   string // "orchestrator" or "workerN"
	agentPicker  AgentPicker
	workerCount  int
	workerAgents []state.AgentType
	workerIdx    int
}

// NewTeamBuilder creates a TeamBuilder.
func NewTeamBuilder() TeamBuilder {
	ti := textinput.New()
	ti.CharLimit = 120
	ti.Width = 40
	return TeamBuilder{
		input:       ti,
		agentPicker: NewAgentPicker(),
		spec: TeamSpec{
			OrchestratorAgent: state.AgentClaude,
			Workers:           []state.AgentType{state.AgentClaude, state.AgentClaude},
		},
	}
}

// Start begins the wizard.
func (tb *TeamBuilder) Start(defaultWorkDir string) {
	tb.Active = true
	tb.step = stepName
	tb.spec = TeamSpec{
		OrchestratorAgent: state.AgentClaude,
		SharedWorkDir:     defaultWorkDir,
	}
	tb.workerCount = 2
	tb.workerAgents = []state.AgentType{state.AgentClaude, state.AgentClaude}
	tb.workerIdx = 0
	tb.input.Reset()
	tb.input.Focus()
}

// Hide dismisses the wizard.
func (tb *TeamBuilder) Hide() {
	tb.Active = false
	tb.input.Blur()
	tb.agentPicker.Hide()
}

// Update processes key input for the wizard.
func (tb *TeamBuilder) Update(msg tea.Msg) tea.Cmd {
	if !tb.Active {
		return nil
	}

	// If agent picker is showing, route to it first.
	if tb.agentPicker.Active {
		cmd, done := tb.agentPicker.Update(msg)
		if done {
			return cmd
		}
		return cmd
	}

	switch m := msg.(type) {
	case AgentPickedMsg:
		if tb.pickerStep == "orchestrator" {
			tb.spec.OrchestratorAgent = m.AgentType
			tb.step = stepWorkerCount
			tb.input.Reset()
			tb.input.SetValue(strconv.Itoa(tb.workerCount))
			tb.input.Focus()
		} else if strings.HasPrefix(tb.pickerStep, "worker") {
			tb.workerAgents[tb.workerIdx] = m.AgentType
			tb.workerIdx++
			if tb.workerIdx >= tb.workerCount {
				tb.step = stepWorkDir
				tb.input.Reset()
				tb.input.SetValue(tb.spec.SharedWorkDir)
				tb.input.Focus()
			} else {
				tb.agentPicker.Show(DefaultAgentItems)
				tb.pickerStep = fmt.Sprintf("worker%d", tb.workerIdx)
			}
		}
		return nil

	case CancelledMsg:
		tb.Hide()
		return nil

	case tea.KeyMsg:
		switch m.String() {
		case "esc":
			tb.Hide()
			return func() tea.Msg { return CancelledMsg{} }

		case "enter":
			return tb.advance()
		}
	}

	var cmd tea.Cmd
	tb.input, cmd = tb.input.Update(msg)
	return cmd
}

func (tb *TeamBuilder) advance() tea.Cmd {
	val := strings.TrimSpace(tb.input.Value())
	switch tb.step {
	case stepName:
		if val == "" {
			return nil
		}
		tb.spec.Name = val
		tb.step = stepGoal
		tb.input.Reset()
		tb.input.Focus()

	case stepGoal:
		tb.spec.Goal = val
		tb.step = stepOrchestrator
		// Launch agent picker for orchestrator
		tb.pickerStep = "orchestrator"
		tb.agentPicker.Show(DefaultAgentItems)
		tb.input.Blur()

	case stepWorkerCount:
		n, err := strconv.Atoi(val)
		if err != nil || n < 1 || n > 10 {
			n = 2
		}
		tb.workerCount = n
		tb.workerAgents = make([]state.AgentType, n)
		for i := range tb.workerAgents {
			tb.workerAgents[i] = state.AgentClaude
		}
		tb.workerIdx = 0
		tb.pickerStep = "worker0"
		tb.agentPicker.Show(DefaultAgentItems)
		tb.input.Blur()

	case stepWorkDir:
		if val != "" {
			tb.spec.SharedWorkDir = val
		}
		tb.step = stepConfirm
		tb.input.Blur()

	case stepConfirm:
		tb.spec.Workers = tb.workerAgents
		tb.Hide()
		return func() tea.Msg { return TeamBuiltMsg{Spec: tb.spec} }
	}
	return nil
}

// View renders the wizard.
func (tb *TeamBuilder) View() string {
	if !tb.Active {
		return ""
	}
	if tb.agentPicker.Active {
		return tb.agentPicker.View()
	}

	var title, prompt, hint string
	switch tb.step {
	case stepName:
		title = "New Team — Step 1/5"
		prompt = "Team name:"
		hint = "enter: next  esc: cancel"
	case stepGoal:
		title = "New Team — Step 2/5"
		prompt = "Team goal (optional):"
		hint = "enter: next  esc: cancel"
	case stepOrchestrator:
		title = "New Team — Step 3/5"
		prompt = "Choosing orchestrator agent…"
		hint = ""
	case stepWorkerCount:
		title = "New Team — Step 4/5"
		prompt = fmt.Sprintf("Number of worker agents (1-10), default %d:", tb.workerCount)
		hint = "enter: next  esc: cancel"
	case stepWorkDir:
		title = "New Team — Step 4.5/5"
		prompt = "Shared working directory:"
		hint = "enter: next  esc: cancel"
	case stepConfirm:
		title = "New Team — Confirm"
		prompt = tb.summaryText()
		hint = "enter: create  esc: cancel"
	}

	content := styles.TitleStyle.Render(title) + "\n\n" +
		prompt + "\n"
	if tb.step != stepConfirm && tb.step != stepOrchestrator {
		content += tb.input.View() + "\n"
	}
	if hint != "" {
		content += "\n" + styles.MutedStyle.Render(hint)
	}

	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.ColorAccent).
		Padding(1, 2).
		Width(56).
		Render(content)
}

func (tb *TeamBuilder) summaryText() string {
	workers := make([]string, len(tb.workerAgents))
	for i, w := range tb.workerAgents {
		workers[i] = fmt.Sprintf("  worker-%d: %s", i+1, styles.AgentBadge(string(w)))
	}
	return fmt.Sprintf(
		"Name: %s\nGoal: %s\nOrchestrator: %s\nWorkers:\n%s\nWork dir: %s\n\n%s",
		tb.spec.Name,
		tb.spec.Goal,
		styles.AgentBadge(string(tb.spec.OrchestratorAgent)),
		strings.Join(workers, "\n"),
		tb.spec.SharedWorkDir,
		styles.TitleStyle.Render("Press enter to create"),
	)
}
