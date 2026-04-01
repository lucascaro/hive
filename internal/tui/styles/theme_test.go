package styles

import (
	"strings"
	"testing"
)

func TestAgentBadge_KnownAgents(t *testing.T) {
	known := []string{"claude", "codex", "gemini", "copilot", "aider", "opencode"}
	for _, agent := range known {
		t.Run(agent, func(t *testing.T) {
			got := AgentBadge(agent)
			if got == "" {
				t.Errorf("AgentBadge(%q) returned empty string", agent)
			}
			if !strings.Contains(got, agent) {
				t.Errorf("AgentBadge(%q) = %q, expected to contain agent name", agent, got)
			}
		})
	}
}

func TestAgentBadge_UnknownAgentFallback(t *testing.T) {
	got := AgentBadge("unknown-agent-xyz")
	if got == "" {
		t.Error("AgentBadge for unknown agent returned empty string")
	}
}

func TestStatusDot_AllStatuses(t *testing.T) {
	statuses := []string{"running", "idle", "waiting", "dead"}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			got := StatusDot(status)
			if got == "" {
				t.Errorf("StatusDot(%q) returned empty string", status)
			}
		})
	}
}

func TestStatusDot_UnknownFallback(t *testing.T) {
	got := StatusDot("unknown-status")
	if got == "" {
		t.Error("StatusDot for unknown status returned empty string")
	}
}

func TestStatusLegend_NonEmpty(t *testing.T) {
	got := StatusLegend()
	if got == "" {
		t.Error("StatusLegend() returned empty string")
	}
}

func TestStatusLegend_ContainsAllStatusNames(t *testing.T) {
	got := StatusLegend()
	for _, label := range []string{"idle", "working", "waiting", "dead"} {
		if !strings.Contains(got, label) {
			t.Errorf("StatusLegend() missing %q", label)
		}
	}
}
