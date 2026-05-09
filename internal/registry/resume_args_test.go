package registry

import (
	"slices"
	"testing"
)

func TestResumeArgsForEntry_UsesResumeCmd_WhenSet(t *testing.T) {
	got := resumeArgsFor("claude", "")
	want := []string{"claude", "--continue"}
	if !slices.Equal(got, want) {
		t.Errorf("claude no convID: got %v, want %v", got, want)
	}
}

func TestResumeArgsForEntry_FallsBackToCmd_WhenResumeEmpty(t *testing.T) {
	// Aider has Cmd but no ResumeCmd → fall back to Cmd.
	got := resumeArgsFor("aider", "")
	want := []string{"aider"}
	if !slices.Equal(got, want) {
		t.Errorf("aider: got %v, want %v", got, want)
	}
}

func TestResumeArgsForEntry_UnknownAgent(t *testing.T) {
	got := resumeArgsFor("nonsense-agent", "")
	if got != nil {
		t.Errorf("unknown agent: got %v, want nil", got)
	}
}

func TestResumeArgsForEntry_PrefersResumeArgsWithID_WhenIDSet(t *testing.T) {
	got := resumeArgsFor("claude", "abc-123")
	want := []string{"claude", "--resume", "abc-123"}
	if !slices.Equal(got, want) {
		t.Errorf("claude with convID: got %v, want %v", got, want)
	}

	got = resumeArgsFor("codex", "abc-123")
	want = []string{"codex", "resume", "abc-123"}
	if !slices.Equal(got, want) {
		t.Errorf("codex with convID: got %v, want %v", got, want)
	}
}

// TestResumeArgsForEntry_FallsBackToResumeCmd_WhenAgentLacksIDSupport
// Gemini has ResumeCmd but no ResumeArgsWithID; convID is ignored.
func TestResumeArgsForEntry_FallsBackToResumeCmd_WhenAgentLacksIDSupport(t *testing.T) {
	got := resumeArgsFor("gemini", "abc-123")
	want := []string{"gemini", "--continue"}
	if !slices.Equal(got, want) {
		t.Errorf("gemini with convID: got %v, want %v", got, want)
	}
}
