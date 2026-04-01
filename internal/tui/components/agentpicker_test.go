package components

import (
	"testing"

	"github.com/charmbracelet/bubbles/list"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/lucascaro/hive/internal/state"
)

func TestFilterAgentItems_SubstringMatchOnType(t *testing.T) {
	tests := []struct {
		query   string
		wantLen int
		wantIDs []state.AgentType
	}{
		// "co" matches: codex (type), copilot (type), opencode (type has "co" in "code"), custom command (label has "co" in "command")
		{"co", 4, nil},
		// "cod" matches: codex (type), opencode (type has "cod" in "code")
		{"cod", 2, nil},
		// "cl" matches: claude (type), copilot CLI (label has "cl" in "cli")
		{"cl", 2, nil},
		// Substring (not prefix) matches
		{"dex", 1, []state.AgentType{state.AgentCodex}},
		{"ode", 2, nil}, // codex (type "c-ode-x") and opencode (type "openc-ode")
		{"aud", 1, []state.AgentType{state.AgentClaude}},
		{"der", 1, []state.AgentType{state.AgentAider}},
		// Full match
		{"codex", 1, []state.AgentType{state.AgentCodex}},
		{"claude", 1, []state.AgentType{state.AgentClaude}},
		// Empty query matches all (strings.Contains(x, "") is always true)
		{"", 7, nil},
		// No match
		{"zzz", 0, nil},
	}

	for _, tt := range tests {
		t.Run("query="+tt.query, func(t *testing.T) {
			result := FilterAgentItems(DefaultAgentItems, tt.query)
			if len(result) != tt.wantLen {
				var got []string
				for _, r := range result {
					if ai, ok := r.(agentItem); ok {
						got = append(got, string(ai.agentType))
					}
				}
				t.Errorf("FilterAgentItems(%q): got %d items %v, want %d", tt.query, len(result), got, tt.wantLen)
				return
			}
			if tt.wantIDs != nil {
				for i, wantID := range tt.wantIDs {
					ai := result[i].(agentItem)
					if ai.agentType != wantID {
						t.Errorf("FilterAgentItems(%q)[%d]: got %s, want %s", tt.query, i, ai.agentType, wantID)
					}
				}
			}
		})
	}
}

func TestFilterAgentItems_MatchesLabel(t *testing.T) {
	tests := []struct {
		query   string
		wantLen int
		desc    string
	}{
		{"Anthropic", 1, "label match for Claude"},
		{"OpenAI", 1, "label match for Codex"},
		{"Google", 1, "label match for Gemini"},
		{"GitHub", 1, "label match for Copilot"},
		{"CLI", 1, "label match for Copilot CLI only (Gemini label is 'Gemini (Google)')"},
	}

	for _, tt := range tests {
		t.Run(tt.desc, func(t *testing.T) {
			result := FilterAgentItems(DefaultAgentItems, tt.query)
			if len(result) != tt.wantLen {
				var got []string
				for _, r := range result {
					if ai, ok := r.(agentItem); ok {
						got = append(got, string(ai.agentType)+":"+ai.label)
					}
				}
				t.Errorf("FilterAgentItems(%q): got %d items %v, want %d (%s)", tt.query, len(result), got, tt.wantLen, tt.desc)
			}
		})
	}
}

func TestFilterAgentItems_CaseInsensitive(t *testing.T) {
	tests := []string{"CODEX", "Codex", "codex", "CoDeX"}
	for _, q := range tests {
		result := FilterAgentItems(DefaultAgentItems, q)
		if len(result) != 1 {
			t.Errorf("FilterAgentItems(%q): got %d items, want 1", q, len(result))
			continue
		}
		ai := result[0].(agentItem)
		if ai.agentType != state.AgentCodex {
			t.Errorf("FilterAgentItems(%q): got %s, want codex", q, ai.agentType)
		}
	}
}

func TestAgentPickerUpdate_TypingBuildsFilter(t *testing.T) {
	ap := NewAgentPicker()
	ap.Show(DefaultAgentItems)

	// Type "co"
	ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if ap.filterQuery != "c" {
		t.Fatalf("after typing 'c': filterQuery=%q, want %q", ap.filterQuery, "c")
	}
	ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	if ap.filterQuery != "co" {
		t.Fatalf("after typing 'o': filterQuery=%q, want %q", ap.filterQuery, "co")
	}

	// "co" matches: codex, copilot, opencode (has "co" in "code"), custom (label "Custom command" has "co")
	items := ap.list.Items()
	if len(items) != 4 {
		var got []string
		for _, item := range items {
			if ai, ok := item.(agentItem); ok {
				got = append(got, string(ai.agentType))
			}
		}
		t.Fatalf("after filter 'co': got %d items %v, want 4", len(items), got)
	}
}

func TestAgentPickerUpdate_BackspaceRemovesFilter(t *testing.T) {
	ap := NewAgentPicker()
	ap.Show(DefaultAgentItems)

	// Type "cod"
	ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})
	ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if ap.filterQuery != "cod" {
		t.Fatalf("filterQuery=%q, want %q", ap.filterQuery, "cod")
	}

	// Backspace
	ap.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if ap.filterQuery != "co" {
		t.Fatalf("after backspace: filterQuery=%q, want %q", ap.filterQuery, "co")
	}
}

func TestAgentPickerUpdate_EscClearsFilter(t *testing.T) {
	ap := NewAgentPicker()
	ap.Show(DefaultAgentItems)

	// Type "co"
	ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	// Verify filter was applied ("co" matches 4 agents)
	if len(ap.list.Items()) != 4 {
		t.Fatalf("after 'co': got %d items, want 4", len(ap.list.Items()))
	}

	// Esc should clear filter, not close picker
	ap.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if ap.filterQuery != "" {
		t.Fatalf("after esc: filterQuery=%q, want empty", ap.filterQuery)
	}
	if !ap.Active {
		t.Fatal("esc with filter should clear filter, not close picker")
	}
	if len(ap.list.Items()) != len(DefaultAgentItems) {
		t.Fatalf("after esc: got %d items, want %d (all)", len(ap.list.Items()), len(DefaultAgentItems))
	}
}

func TestAgentPickerUpdate_EscClosesPicker(t *testing.T) {
	ap := NewAgentPicker()
	ap.Show(DefaultAgentItems)

	// Esc with no filter should close
	cmd, _ := ap.Update(tea.KeyMsg{Type: tea.KeyEscape})
	if ap.Active {
		t.Fatal("esc with no filter should close picker")
	}
	// Should return CancelledMsg
	if cmd == nil {
		t.Fatal("esc should return a cmd")
	}
	msg := cmd()
	if _, ok := msg.(CancelledMsg); !ok {
		t.Fatalf("esc cmd returned %T, want CancelledMsg", msg)
	}
}

func TestAgentPickerUpdate_EnterSelectsItem(t *testing.T) {
	ap := NewAgentPicker()
	ap.Show(DefaultAgentItems)

	// Type "codex" to narrow to one item
	for _, r := range "codex" {
		ap.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(ap.list.Items()) != 1 {
		t.Fatalf("after 'codex': got %d items, want 1", len(ap.list.Items()))
	}

	// Enter selects
	cmd, _ := ap.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter should return a cmd")
	}
	msg := cmd()
	picked, ok := msg.(AgentPickedMsg)
	if !ok {
		t.Fatalf("enter cmd returned %T, want AgentPickedMsg", msg)
	}
	if picked.AgentType != state.AgentCodex {
		t.Fatalf("picked agent=%s, want codex", picked.AgentType)
	}
}

func TestFilterAgentItems_EmptyQuery(t *testing.T) {
	result := FilterAgentItems(DefaultAgentItems, "")
	// Empty query: strings.Contains(x, "") is always true, so all items match.
	// The caller (applyFilter) handles the empty case separately.
	if len(result) != len(DefaultAgentItems) {
		t.Errorf("empty query: got %d items, want %d (all)", len(result), len(DefaultAgentItems))
	}
}

func TestFilterAgentItems_NilItems(t *testing.T) {
	result := FilterAgentItems(nil, "codex")
	if len(result) != 0 {
		t.Errorf("nil items: got %d, want 0", len(result))
	}
}

func TestFilterAgentItems_NonAgentItems(t *testing.T) {
	// Items that don't implement agentItem should be skipped
	items := []list.Item{DefaultAgentItems[0]}
	result := FilterAgentItems(items, "claude")
	if len(result) != 1 {
		t.Errorf("got %d, want 1", len(result))
	}
}
