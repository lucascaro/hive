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

// --- SetProjectColor ---

func TestSetProjectColor(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "#FF0000", "")
	if p.Color != "#FF0000" {
		t.Fatalf("initial color = %q, want #FF0000", p.Color)
	}
	s = SetProjectColor(s, p.ID, "#00FF00")
	if p.Color != "#00FF00" {
		t.Errorf("after SetProjectColor: color = %q, want #00FF00", p.Color)
	}
}

func TestSetProjectColor_UnknownID(t *testing.T) {
	s := emptyState()
	s, _ = CreateProject(s, "p", "", "#FF0000", "")
	// Should not panic on unknown ID.
	s = SetProjectColor(s, "nonexistent", "#00FF00")
	if s.Projects[0].Color != "#FF0000" {
		t.Error("SetProjectColor with unknown ID should not modify existing projects")
	}
}

// --- SetSessionColor ---

func TestSetSessionColor_Standalone(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "#FF0000", "")
	s, sess := CreateSession(s, p.ID, "s1", AgentClaude, nil, "/work", "tmux", 0)
	if sess.Color != "" {
		t.Fatalf("initial session color = %q, want empty", sess.Color)
	}
	s = SetSessionColor(s, sess.ID, "#00FF00")
	if sess.Color != "#00FF00" {
		t.Errorf("after SetSessionColor: color = %q, want #00FF00", sess.Color)
	}
}

func TestSetSessionColor_TeamSession(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "#FF0000", "")
	s, team := CreateTeam(s, p.ID, "team1", "goal", "/work")
	s, sess := AddTeamSession(s, p.ID, team.ID, RoleWorker, "w1", AgentClaude, nil, "/work", "tmux", 0)
	s = SetSessionColor(s, sess.ID, "#0000FF")
	if sess.Color != "#0000FF" {
		t.Errorf("team session color = %q, want #0000FF", sess.Color)
	}
}

func TestSetSessionColor_UnknownID(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "#FF0000", "")
	s, sess := CreateSession(s, p.ID, "s1", AgentClaude, nil, "/work", "tmux", 0)
	// Should not panic on unknown ID.
	s = SetSessionColor(s, "nonexistent", "#00FF00")
	if sess.Color != "" {
		t.Error("SetSessionColor with unknown ID should not modify existing sessions")
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

// --- UpdateProjectName ---

func TestUpdateProjectName_UpdatesName(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "old-name", "", "", "/work")
	s = UpdateProjectName(s, p.ID, "new-name")
	if s.Projects[0].Name != "new-name" {
		t.Errorf("Name = %q, want %q", s.Projects[0].Name, "new-name")
	}
}

func TestUpdateProjectName_UnknownID(t *testing.T) {
	s := emptyState()
	s, _ = CreateProject(s, "orig", "", "", "/work")
	s = UpdateProjectName(s, "nonexistent", "new")
	if s.Projects[0].Name != "orig" {
		t.Errorf("Name = %q, want %q", s.Projects[0].Name, "orig")
	}
}

// --- UpdateTeamName ---

func TestUpdateTeamName_UpdatesName(t *testing.T) {
	s := emptyState()
	s, _ = CreateProject(s, "proj", "", "", "/work")
	s, team := CreateTeam(s, s.Projects[0].ID, "old-team", "", "/work")
	s = UpdateTeamName(s, team.ID, "new-team")
	if s.Projects[0].Teams[0].Name != "new-team" {
		t.Errorf("Name = %q, want %q", s.Projects[0].Teams[0].Name, "new-team")
	}
}

func TestUpdateTeamName_UnknownID(t *testing.T) {
	s := emptyState()
	s, _ = CreateProject(s, "proj", "", "", "/work")
	s, _ = CreateTeam(s, s.Projects[0].ID, "orig", "", "/work")
	s = UpdateTeamName(s, "nonexistent", "new")
	if s.Projects[0].Teams[0].Name != "orig" {
		t.Errorf("Name = %q, want %q", s.Projects[0].Teams[0].Name, "orig")
	}
}

// --- NextSessionAfterRemoval ---

func TestNextSessionAfterRemoval_NextStandalone(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, s1 := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, s2 := CreateSession(s, p.ID, "s2", AgentClaude, nil, "", "h", 1)
	s, _ = CreateSession(s, p.ID, "s3", AgentClaude, nil, "", "h", 2)
	// Removing s1 → next in group is s2
	got := NextSessionAfterRemoval(s, s1.ID)
	if got != s2.ID {
		t.Errorf("got %q, want %q (next in group)", got, s2.ID)
	}
}

func TestNextSessionAfterRemoval_PrevStandalone(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, s1 := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, s2 := CreateSession(s, p.ID, "s2", AgentClaude, nil, "", "h", 1)
	// Removing s2 (last) → prev in group is s1
	got := NextSessionAfterRemoval(s, s2.ID)
	if got != s1.ID {
		t.Errorf("got %q, want %q (prev in group)", got, s1.ID)
	}
}

func TestNextSessionAfterRemoval_NextInTeam(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, t1 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t1", AgentClaude, nil, "", "h", 0)
	s, t2 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t2", AgentClaude, nil, "", "h", 1)
	got := NextSessionAfterRemoval(s, t1.ID)
	if got != t2.ID {
		t.Errorf("got %q, want %q (next in team)", got, t2.ID)
	}
}

func TestNextSessionAfterRemoval_PrevInTeam(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, t1 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t1", AgentClaude, nil, "", "h", 0)
	s, t2 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t2", AgentClaude, nil, "", "h", 1)
	got := NextSessionAfterRemoval(s, t2.ID)
	if got != t1.ID {
		t.Errorf("got %q, want %q (prev in team)", got, t1.ID)
	}
}

func TestNextSessionAfterRemoval_CrossProjectFallback(t *testing.T) {
	s := emptyState()
	s, p1 := CreateProject(s, "p1", "", "", "")
	s, s1 := CreateSession(s, p1.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, p2 := CreateProject(s, "p2", "", "", "")
	s, s2 := CreateSession(s, p2.ID, "s2", AgentClaude, nil, "", "h", 1)
	// s1 is only session in p1 → no group neighbor → falls back to overall next = s2
	got := NextSessionAfterRemoval(s, s1.ID)
	if got != s2.ID {
		t.Errorf("got %q, want %q (cross-project fallback)", got, s2.ID)
	}
}

func TestNextSessionAfterRemoval_SingleSessionGlobally(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "s", AgentClaude, nil, "", "h", 0)
	got := NextSessionAfterRemoval(s, sess.ID)
	if got != "" {
		t.Errorf("got %q, want empty (single session)", got)
	}
}

func TestNextSessionAfterRemoval_NoSessions(t *testing.T) {
	s := emptyState()
	got := NextSessionAfterRemoval(s, "nonexistent")
	if got != "" {
		t.Errorf("got %q, want empty (no sessions)", got)
	}
}

func TestNextSessionAfterRemoval_OnlySessionInTeam_FallsBackOverall(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, t1 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t1", AgentClaude, nil, "", "h", 0)
	s, standalone := CreateSession(s, p.ID, "standalone", AgentClaude, nil, "", "h", 1)
	// t1 is only session in team → no group neighbor → falls back to overall
	got := NextSessionAfterRemoval(s, t1.ID)
	if got != standalone.ID {
		t.Errorf("got %q, want %q (fallback from team to overall)", got, standalone.ID)
	}
}

// TestNextSessionAfterRemoval_OverallUsesUIOrder verifies that the overall
// fallback walks sessions in sidebar render order (teams before standalones
// within a project), not in AllSessions flattening order.
func TestNextSessionAfterRemoval_OverallUsesUIOrder(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	// Standalone comes before team in append order, but sidebar renders team first.
	s, standalone := CreateSession(s, p.ID, "standalone", AgentClaude, nil, "", "h", 0)
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, t1 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t1", AgentClaude, nil, "", "h", 1)

	// Remove standalone (only session in its group). Next in UI order after
	// standalone is... nothing (standalones render last). Prev in UI order
	// is t1 (team session rendered before standalone). Expect t1.
	got := NextSessionAfterRemoval(s, standalone.ID)
	if got != t1.ID {
		t.Errorf("got %q, want %q (prev in UI order should be team session)", got, t1.ID)
	}
}

func TestSessionLabel_OrchestratorHasStar(t *testing.T) {
	sess := &Session{Title: "orch", AgentType: AgentClaude, TeamRole: RoleOrchestrator}
	label := SessionLabel(sess)
	if !strings.HasPrefix(label, "★ ") {
		t.Errorf("SessionLabel for orchestrator = %q, want ★ prefix", label)
	}
}

func TestSessionLabel_ContainsAgentType(t *testing.T) {
	sess := &Session{Title: "test", AgentType: AgentCodex, TeamRole: RoleStandalone}
	label := SessionLabel(sess)
	if !strings.Contains(label, "[codex]") {
		t.Errorf("SessionLabel = %q, want to contain [codex]", label)
	}
}

func TestSessionLabel_WorkerNoStar(t *testing.T) {
	sess := &Session{Title: "worker", AgentType: AgentClaude, TeamRole: RoleWorker}
	label := SessionLabel(sess)
	if strings.HasPrefix(label, "★") {
		t.Errorf("SessionLabel for worker = %q, should not start with ★", label)
	}
}

// --- FindSession ---

func TestFindSession_Standalone(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "hive-abc", 0)
	found := FindSession(s, sess.ID)
	if found == nil {
		t.Fatal("FindSession returned nil, want session")
	}
	if found.ID != sess.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, sess.ID)
	}
}

func TestFindSession_InTeam(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, sess := AddTeamSession(s, p.ID, team.ID, RoleWorker, "w", AgentClaude, nil, "", "hive-abc", 0)
	found := FindSession(s, sess.ID)
	if found == nil {
		t.Fatal("FindSession returned nil for team session")
	}
	if found.ID != sess.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, sess.ID)
	}
}

func TestFindSession_NotFound(t *testing.T) {
	s := emptyState()
	if FindSession(s, "nonexistent") != nil {
		t.Error("FindSession should return nil for unknown ID")
	}
}

func TestFindSession_EmptyState(t *testing.T) {
	s := &AppState{}
	if FindSession(s, "any") != nil {
		t.Error("FindSession should return nil on empty state")
	}
}

// --- FindProject ---

func TestFindProject_Found(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "proj", "", "", "")
	found := FindProject(s, p.ID)
	if found == nil {
		t.Fatal("FindProject returned nil")
	}
	if found.Name != "proj" {
		t.Errorf("found.Name = %q, want %q", found.Name, "proj")
	}
}

func TestFindProject_NotFound(t *testing.T) {
	s := emptyState()
	s, _ = CreateProject(s, "proj", "", "", "")
	if FindProject(s, "nonexistent") != nil {
		t.Error("FindProject should return nil for unknown ID")
	}
}

func TestFindProject_EmptyState(t *testing.T) {
	s := emptyState()
	if FindProject(s, "any") != nil {
		t.Error("FindProject should return nil on empty state")
	}
}

// --- FindTeam ---

func TestFindTeam_Found(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "my-team", "", "")
	found := FindTeam(s, team.ID)
	if found == nil {
		t.Fatal("FindTeam returned nil")
	}
	if found.Name != "my-team" {
		t.Errorf("found.Name = %q, want %q", found.Name, "my-team")
	}
}

func TestFindTeam_NotFound(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = CreateTeam(s, p.ID, "team", "", "")
	if FindTeam(s, "nonexistent") != nil {
		t.Error("FindTeam should return nil for unknown ID")
	}
}

func TestFindTeam_EmptyState(t *testing.T) {
	s := emptyState()
	if FindTeam(s, "any") != nil {
		t.Error("FindTeam should return nil on empty state")
	}
}

func TestFindTeam_MultipleProjects(t *testing.T) {
	s := emptyState()
	s, p1 := CreateProject(s, "p1", "", "", "")
	s, _ = CreateTeam(s, p1.ID, "team-a", "", "")
	s, p2 := CreateProject(s, "p2", "", "", "")
	s, teamB := CreateTeam(s, p2.ID, "team-b", "", "")
	found := FindTeam(s, teamB.ID)
	if found == nil {
		t.Fatal("FindTeam returned nil for team in second project")
	}
	if found.Name != "team-b" {
		t.Errorf("found.Name = %q, want %q", found.Name, "team-b")
	}
}

// --- FindSessionByTmux ---

func TestFindSessionByTmux_Found(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "hive-abc", 3)
	found := FindSessionByTmux(s, "hive-abc", 3)
	if found == nil {
		t.Fatal("FindSessionByTmux returned nil")
	}
	if found.ID != sess.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, sess.ID)
	}
}

func TestFindSessionByTmux_WrongWindow(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "hive-abc", 3)
	if FindSessionByTmux(s, "hive-abc", 99) != nil {
		t.Error("FindSessionByTmux should return nil when window doesn't match")
	}
}

func TestFindSessionByTmux_WrongSession(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "hive-abc", 3)
	if FindSessionByTmux(s, "hive-xyz", 3) != nil {
		t.Error("FindSessionByTmux should return nil when session doesn't match")
	}
}

func TestFindSessionByTmux_InTeam(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, sess := AddTeamSession(s, p.ID, team.ID, RoleWorker, "w", AgentClaude, nil, "", "hive-abc", 5)
	found := FindSessionByTmux(s, "hive-abc", 5)
	if found == nil {
		t.Fatal("FindSessionByTmux returned nil for team session")
	}
	if found.ID != sess.ID {
		t.Errorf("found.ID = %q, want %q", found.ID, sess.ID)
	}
}

func TestFindSessionByTmux_NotFound(t *testing.T) {
	s := emptyState()
	if FindSessionByTmux(s, "hive-abc", 0) != nil {
		t.Error("FindSessionByTmux should return nil on empty state")
	}
}

// --- AllSessions (additional) ---

func TestAllSessions_EmptyState(t *testing.T) {
	s := emptyState()
	all := AllSessions(s)
	if len(all) != 0 {
		t.Errorf("AllSessions on empty state: len = %d, want 0", len(all))
	}
}

func TestAllSessions_StandaloneOnly(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "hive-abc", 0)
	s, _ = CreateSession(s, p.ID, "s2", AgentClaude, nil, "", "hive-abc", 1)
	all := AllSessions(s)
	if len(all) != 2 {
		t.Errorf("AllSessions len = %d, want 2", len(all))
	}
}

func TestAllSessions_TeamOnly(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, _ = AddTeamSession(s, p.ID, team.ID, RoleOrchestrator, "o", AgentClaude, nil, "", "hive-abc", 0)
	s, _ = AddTeamSession(s, p.ID, team.ID, RoleWorker, "w", AgentClaude, nil, "", "hive-abc", 1)
	all := AllSessions(s)
	if len(all) != 2 {
		t.Errorf("AllSessions len = %d, want 2", len(all))
	}
}

// --- RecordAgentUsage (additional) ---

func TestRecordAgentUsage_MultipleAgents(t *testing.T) {
	s := emptyState()
	RecordAgentUsage(s, "claude")
	RecordAgentUsage(s, "codex")
	RecordAgentUsage(s, "claude")
	if s.AgentUsage["claude"].Count != 2 {
		t.Errorf("claude Count = %d, want 2", s.AgentUsage["claude"].Count)
	}
	if s.AgentUsage["codex"].Count != 1 {
		t.Errorf("codex Count = %d, want 1", s.AgentUsage["codex"].Count)
	}
}

func TestRecordAgentUsage_SetsLastUsed(t *testing.T) {
	s := emptyState()
	RecordAgentUsage(s, "claude")
	rec := s.AgentUsage["claude"]
	if rec.LastUsed.IsZero() {
		t.Error("LastUsed should not be zero")
	}
}

// --- MoveSessionUp / MoveSessionDown ---

func TestMoveSessionDown_MiddleStandalone(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, s1 := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, s2 := CreateSession(s, p.ID, "s2", AgentClaude, nil, "", "h", 1)
	s, s3 := CreateSession(s, p.ID, "s3", AgentClaude, nil, "", "h", 2)
	s, _ = MoveSessionDown(s, s1.ID)
	if p.Sessions[0].ID != s2.ID || p.Sessions[1].ID != s1.ID || p.Sessions[2].ID != s3.ID {
		t.Errorf("order after MoveSessionDown(s1): got [%s,%s,%s] want [%s,%s,%s]",
			p.Sessions[0].Title, p.Sessions[1].Title, p.Sessions[2].Title,
			s2.Title, s1.Title, s3.Title)
	}
}

func TestMoveSessionUp_MiddleStandalone(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, s1 := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, s2 := CreateSession(s, p.ID, "s2", AgentClaude, nil, "", "h", 1)
	s, s3 := CreateSession(s, p.ID, "s3", AgentClaude, nil, "", "h", 2)
	s, _ = MoveSessionUp(s, s3.ID)
	if p.Sessions[0].ID != s1.ID || p.Sessions[1].ID != s3.ID || p.Sessions[2].ID != s2.ID {
		t.Errorf("order after MoveSessionUp(s3): got [%s,%s,%s] want [%s,%s,%s]",
			p.Sessions[0].Title, p.Sessions[1].Title, p.Sessions[2].Title,
			s1.Title, s3.Title, s2.Title)
	}
}

func TestMoveSessionDown_LastIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, s1 := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, s2 := CreateSession(s, p.ID, "s2", AgentClaude, nil, "", "h", 1)
	s, _ = MoveSessionDown(s, s2.ID)
	if p.Sessions[0].ID != s1.ID || p.Sessions[1].ID != s2.ID {
		t.Error("MoveSessionDown on last session should be no-op")
	}
}

func TestMoveSessionUp_FirstIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, s1 := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, s2 := CreateSession(s, p.ID, "s2", AgentClaude, nil, "", "h", 1)
	s, _ = MoveSessionUp(s, s1.ID)
	if p.Sessions[0].ID != s1.ID || p.Sessions[1].ID != s2.ID {
		t.Error("MoveSessionUp on first session should be no-op")
	}
}

func TestMoveSessionDown_SingleIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, sess := CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, _ = MoveSessionDown(s, sess.ID)
	if len(p.Sessions) != 1 || p.Sessions[0].ID != sess.ID {
		t.Error("MoveSessionDown on single session should be no-op")
	}
}

func TestMoveSessionUp_TeamSession(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, t1 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t1", AgentClaude, nil, "", "h", 0)
	s, t2 := AddTeamSession(s, p.ID, team.ID, RoleWorker, "t2", AgentClaude, nil, "", "h", 1)
	s, _ = MoveSessionUp(s, t2.ID)
	if team.Sessions[0].ID != t2.ID || team.Sessions[1].ID != t1.ID {
		t.Errorf("order after MoveSessionUp(t2): got [%s,%s] want [%s,%s]",
			team.Sessions[0].Title, team.Sessions[1].Title, t2.Title, t1.Title)
	}
}

func TestMoveSessionDown_NotFoundIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = CreateSession(s, p.ID, "s1", AgentClaude, nil, "", "h", 0)
	s, _ = MoveSessionDown(s, "nonexistent")
	if len(p.Sessions) != 1 {
		t.Error("MoveSessionDown with unknown ID should be no-op")
	}
}

// --- MoveTeamUp / MoveTeamDown ---

func TestMoveTeamDown_SwapsTeams(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team1 := CreateTeam(s, p.ID, "team1", "", "")
	s, team2 := CreateTeam(s, p.ID, "team2", "", "")
	s, _ = MoveTeamDown(s, team1.ID)
	if p.Teams[0].ID != team2.ID || p.Teams[1].ID != team1.ID {
		t.Errorf("order after MoveTeamDown(team1): got [%s,%s] want [%s,%s]",
			p.Teams[0].Name, p.Teams[1].Name, team2.Name, team1.Name)
	}
}

func TestMoveTeamUp_SwapsTeams(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team1 := CreateTeam(s, p.ID, "team1", "", "")
	s, team2 := CreateTeam(s, p.ID, "team2", "", "")
	s, _ = MoveTeamUp(s, team2.ID)
	if p.Teams[0].ID != team2.ID || p.Teams[1].ID != team1.ID {
		t.Errorf("order after MoveTeamUp(team2): got [%s,%s] want [%s,%s]",
			p.Teams[0].Name, p.Teams[1].Name, team2.Name, team1.Name)
	}
}

func TestMoveTeamDown_LastIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = CreateTeam(s, p.ID, "team1", "", "")
	s, team2 := CreateTeam(s, p.ID, "team2", "", "")
	s, _ = MoveTeamDown(s, team2.ID)
	if p.Teams[1].Name != "team2" {
		t.Error("MoveTeamDown on last team should be no-op")
	}
}

func TestMoveTeamUp_FirstIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team1 := CreateTeam(s, p.ID, "team1", "", "")
	s, _ = CreateTeam(s, p.ID, "team2", "", "")
	s, _ = MoveTeamUp(s, team1.ID)
	if p.Teams[0].Name != "team1" {
		t.Error("MoveTeamUp on first team should be no-op")
	}
}

func TestMoveTeamDown_SingleIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, team := CreateTeam(s, p.ID, "team", "", "")
	s, _ = MoveTeamDown(s, team.ID)
	if len(p.Teams) != 1 || p.Teams[0].ID != team.ID {
		t.Error("MoveTeamDown on single team should be no-op")
	}
}

// --- MoveProjectUp / MoveProjectDown ---

func TestMoveProjectDown_SwapsProjects(t *testing.T) {
	s := emptyState()
	s, p1 := CreateProject(s, "p1", "", "", "")
	s, p2 := CreateProject(s, "p2", "", "", "")
	s, _ = MoveProjectDown(s, p1.ID)
	if s.Projects[0].ID != p2.ID || s.Projects[1].ID != p1.ID {
		t.Errorf("order after MoveProjectDown(p1): got [%s,%s] want [%s,%s]",
			s.Projects[0].Name, s.Projects[1].Name, p2.Name, p1.Name)
	}
}

func TestMoveProjectUp_SwapsProjects(t *testing.T) {
	s := emptyState()
	s, p1 := CreateProject(s, "p1", "", "", "")
	s, p2 := CreateProject(s, "p2", "", "", "")
	s, _ = MoveProjectUp(s, p2.ID)
	if s.Projects[0].ID != p2.ID || s.Projects[1].ID != p1.ID {
		t.Errorf("order after MoveProjectUp(p2): got [%s,%s] want [%s,%s]",
			s.Projects[0].Name, s.Projects[1].Name, p2.Name, p1.Name)
	}
}

func TestMoveProjectDown_LastIsNoOp(t *testing.T) {
	s := emptyState()
	s, _ = CreateProject(s, "p1", "", "", "")
	s, p2 := CreateProject(s, "p2", "", "", "")
	s, _ = MoveProjectDown(s, p2.ID)
	if s.Projects[1].Name != "p2" {
		t.Error("MoveProjectDown on last project should be no-op")
	}
}

func TestMoveProjectUp_FirstIsNoOp(t *testing.T) {
	s := emptyState()
	s, p1 := CreateProject(s, "p1", "", "", "")
	s, _ = CreateProject(s, "p2", "", "", "")
	s, _ = MoveProjectUp(s, p1.ID)
	if s.Projects[0].Name != "p1" {
		t.Error("MoveProjectUp on first project should be no-op")
	}
}

func TestMoveProjectDown_SingleIsNoOp(t *testing.T) {
	s := emptyState()
	s, p := CreateProject(s, "p", "", "", "")
	s, _ = MoveProjectDown(s, p.ID)
	if len(s.Projects) != 1 || s.Projects[0].ID != p.ID {
		t.Error("MoveProjectDown on single project should be no-op")
	}
}

func TestMoveProjectUp_ThreeProjects_MiddleMovesUp(t *testing.T) {
	s := emptyState()
	s, p1 := CreateProject(s, "p1", "", "", "")
	s, p2 := CreateProject(s, "p2", "", "", "")
	s, p3 := CreateProject(s, "p3", "", "", "")
	s, _ = MoveProjectUp(s, p2.ID)
	if s.Projects[0].ID != p2.ID || s.Projects[1].ID != p1.ID || s.Projects[2].ID != p3.ID {
		t.Errorf("order after MoveProjectUp(p2): got [%s,%s,%s] want [%s,%s,%s]",
			s.Projects[0].Name, s.Projects[1].Name, s.Projects[2].Name,
			p2.Name, p1.Name, p3.Name)
	}
}
