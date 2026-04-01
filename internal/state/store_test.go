package state

import (
	"strings"
	"testing"
)

func emptyState() *AppState {
	return &AppState{
		Projects:   []*Project{},
		AgentUsage: make(map[string]AgentUsageRecord),
	}
}

// --- CreateProject ---

func TestCreateProject_AddsProject(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "my-proj", "desc", "blue", "/work")
	if len(s.Projects) != 1 {
		t.Fatalf("Projects len = %d, want 1", len(s.Projects))
	}
	if p.Name != "my-proj" {
		t.Errorf("Name = %q, want %q", p.Name, "my-proj")
	}
	if p.ID == "" {
		t.Error("ID should not be empty")
	}
	if s.ActiveProjectID != p.ID {
		t.Errorf("ActiveProjectID = %q, want %q", s.ActiveProjectID, p.ID)
	}
}

func TestCreateProject_NonNilSlices(t *testing.T) {
	s := emptyState()
	_, p := CreateProject(s, "p", "", "", "")
	if p.Teams == nil {
		t.Error("Teams should not be nil")
	}
	if p.Sessions == nil {
		t.Error("Sessions should not be nil")
	}
}

// --- RemoveProject ---

func TestRemoveProject_RemovesProject(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s = RemoveProject(s, p.ID)
	if len(s.Projects) != 0 {
		t.Errorf("Projects len = %d after remove, want 0", len(s.Projects))
	}
}

func TestRemoveProject_ClearsActiveIDs(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s.ActiveProjectID = p.ID
	s.ActiveSessionID = "some-session"
	s.ActiveTeamID = "some-team"
	s = RemoveProject(s, p.ID)
	if s.ActiveProjectID != "" {
		t.Errorf("ActiveProjectID = %q after remove, want empty", s.ActiveProjectID)
	}
	if s.ActiveSessionID != "" {
		t.Errorf("ActiveSessionID = %q after remove, want empty", s.ActiveSessionID)
	}
}

func TestRemoveProject_OtherProjectsUnaffected(t *testing.T) {
	s := emptyState()
	s, p1 := CreateProject(s, "p1", "", "", "")
	s, _ = CreateProject(s, "p2", "", "", "")
	s = RemoveProject(s, p1.ID)
	if len(s.Projects) != 1 {
		t.Errorf("Projects len = %d, want 1", len(s.Projects))
	}
	if s.Projects[0].Name != "p2" {
		t.Errorf("Remaining project = %q, want p2", s.Projects[0].Name)
	}
}

// --- CreateSession ---

func TestCreateSession_AddsToProject(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "my-session", AgentClaude, []string{"claude"}, "/work", "hive-abc", 0)
	if len(p.Sessions) != 1 {
		t.Fatalf("Sessions len = %d, want 1", len(p.Sessions))
	}
	if sess.Title != "my-session" {
		t.Errorf("Title = %q, want %q", sess.Title, "my-session")
	}
	if s.ActiveSessionID != sess.ID {
		t.Errorf("ActiveSessionID = %q, want %q", s.ActiveSessionID, sess.ID)
	}
}

// --- CreateTeam ---

func TestCreateTeam_AddsToProject(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "my-team", "the goal", "/work")
	if len(p.Teams) != 1 {
		t.Fatalf("Teams len = %d, want 1", len(p.Teams))
	}
	if team.Name != "my-team" {
		t.Errorf("Name = %q, want %q", team.Name, "my-team")
	}
	if s.ActiveTeamID != team.ID {
		t.Errorf("ActiveTeamID = %q, want %q", s.ActiveTeamID, team.ID)
	}
}

// --- AddTeamSession ---

func TestAddTeamSession_AddsToTeam(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, sess := AddTeamSession(s, p.ID, team.ID, RoleOrchestrator, "orch", AgentClaude, []string{"claude"}, "", "hive-abc", 0)
	if len(team.Sessions) != 1 {
		t.Fatalf("team.Sessions len = %d, want 1", len(team.Sessions))
	}
	if sess.TeamRole != RoleOrchestrator {
		t.Errorf("TeamRole = %q, want %q", sess.TeamRole, RoleOrchestrator)
	}
}

func TestAddTeamSession_OrchestratorSetsID(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, sess := AddTeamSession(s, p.ID, team.ID, RoleOrchestrator, "orch", AgentClaude, []string{"claude"}, "", "hive-abc", 0)
	_ = s
	if team.OrchestratorID != sess.ID {
		t.Errorf("OrchestratorID = %q, want %q", team.OrchestratorID, sess.ID)
	}
}

func TestAddTeamSession_WorkerDoesNotSetOrchestratorID(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, _ = AddTeamSession(s, p.ID, team.ID, RoleWorker, "worker", AgentClaude, []string{"claude"}, "", "hive-abc", 0)
	_ = s
	if team.OrchestratorID != "" {
		t.Errorf("OrchestratorID = %q for worker, want empty", team.OrchestratorID)
	}
}

// --- RemoveSession ---

func TestRemoveSession_FromProject(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "s", AgentClaude, nil, "", "hive-abc", 0)
	s = RemoveSession(s, sess.ID)
	if len(p.Sessions) != 0 {
		t.Errorf("Sessions len = %d after remove, want 0", len(p.Sessions))
	}
	if s.ActiveSessionID != "" {
		t.Errorf("ActiveSessionID = %q after remove, want empty", s.ActiveSessionID)
	}
}

func TestRemoveSession_FromTeam(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, sess := AddTeamSession(s, p.ID, team.ID, RoleWorker, "w", AgentClaude, nil, "", "hive-abc", 0)
	s = RemoveSession(s, sess.ID)
	if len(team.Sessions) != 0 {
		t.Errorf("team.Sessions len = %d after remove, want 0", len(team.Sessions))
	}
}

// --- RemoveTeam ---

func TestRemoveTeam_RemovesTeam(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s = RemoveTeam(s, team.ID)
	if len(p.Teams) != 0 {
		t.Errorf("Teams len = %d after remove, want 0", len(p.Teams))
	}
	if s.ActiveTeamID != "" {
		t.Errorf("ActiveTeamID = %q after remove, want empty", s.ActiveTeamID)
	}
}

// --- UpdateSessionTitle ---

func TestUpdateSessionTitle(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "old", AgentClaude, nil, "", "hive-abc", 0)
	s = UpdateSessionTitle(s, sess.ID, "new title", TitleSourceUser)
	updated := FindSession(s, sess.ID)
	if updated.Title != "new title" {
		t.Errorf("Title = %q, want %q", updated.Title, "new title")
	}
	if updated.TitleSource != TitleSourceUser {
		t.Errorf("TitleSource = %q, want %q", updated.TitleSource, TitleSourceUser)
	}
}

func TestUpdateSessionTitle_UnknownIDNoOp(t *testing.T) {
	s := emptyState()
	// Should not panic
	s = UpdateSessionTitle(s, "nonexistent", "title", TitleSourceAuto)
	_ = s
}

// --- UpdateSessionStatus ---

func TestUpdateSessionStatus(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "s", AgentClaude, nil, "", "hive-abc", 0)
	s = UpdateSessionStatus(s, sess.ID, StatusIdle)
	updated := FindSession(s, sess.ID)
	if updated.Status != StatusIdle {
		t.Errorf("Status = %q, want %q", updated.Status, StatusIdle)
	}
}

// --- ToggleProjectCollapsed ---

func TestToggleProjectCollapsed(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	if p.Collapsed {
		t.Fatal("project should not be collapsed initially")
	}
	s = ToggleProjectCollapsed(s, p.ID)
	if !p.Collapsed {
		t.Error("project should be collapsed after toggle")
	}
	s = ToggleProjectCollapsed(s, p.ID)
	if p.Collapsed {
		t.Error("project should not be collapsed after second toggle")
	}
}

// --- ToggleTeamCollapsed ---

func TestToggleTeamCollapsed(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s = ToggleTeamCollapsed(s, team.ID)
	if !team.Collapsed {
		t.Error("team should be collapsed after toggle")
	}
	s = ToggleTeamCollapsed(s, team.ID)
	if team.Collapsed {
		t.Error("team should not be collapsed after second toggle")
	}
}

// --- AllSessions ---

func TestAllSessions(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "hive-abc", 0)
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, _ = AddTeamSession(s, p.ID, team.ID, RoleWorker, "s2", AgentClaude, nil, "", "hive-abc", 1)
	all := AllSessions(s)
	if len(all) != 2 {
		t.Errorf("AllSessions() len = %d, want 2", len(all))
	}
}

// --- RecordAgentUsage ---

func TestRecordAgentUsage_IncreasesCount(t *testing.T) {
	s := emptyState()
	RecordAgentUsage(s, "claude")
	RecordAgentUsage(s, "claude")
	rec := s.AgentUsage["claude"]
	if rec.Count != 2 {
		t.Errorf("Count = %d, want 2", rec.Count)
	}
}

func TestRecordAgentUsage_NilMapInitialized(t *testing.T) {
	s := &AppState{AgentUsage: nil}
	RecordAgentUsage(s, "claude")
	if s.AgentUsage == nil {
		t.Error("AgentUsage should be initialized")
	}
}

// --- SessionLabel ---

func TestSessionLabel_StandaloneNoStar(t *testing.T) {
	sess := &Session{Title: "my-session", AgentType: AgentClaude, TeamRole: RoleStandalone}
	label := SessionLabel(sess)
	if label == "" {
		t.Error("SessionLabel should not be empty")
	}
	// Standalone should not have the star prefix
	if strings.HasPrefix(label, "★") {
		t.Errorf("SessionLabel for standalone = %q, should not start with ★", label)
	}
}

func TestSessionLabel_OrchestratorHasStar(t *testing.T) {
	sess := &Session{Title: "orch", AgentType: AgentClaude, TeamRole: RoleOrchestrator}
	label := SessionLabel(sess)
	if !strings.HasPrefix(label, "★ ") {
		t.Errorf("SessionLabel for orchestrator = %q, want ★ prefix", label)
	}
}
