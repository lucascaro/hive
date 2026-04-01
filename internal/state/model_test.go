package state

import (
	"math"
	"testing"
	"time"
)

func TestAgentUsageRecord_Score_IncreasesWithCount(t *testing.T) {
	r1 := AgentUsageRecord{Count: 1, LastUsed: time.Now()}
	r2 := AgentUsageRecord{Count: 10, LastUsed: time.Now()}
	if r2.Score() <= r1.Score() {
		t.Errorf("higher count should yield higher score: score(1)=%f, score(10)=%f", r1.Score(), r2.Score())
	}
}

func TestAgentUsageRecord_Score_PositiveValue(t *testing.T) {
	r := AgentUsageRecord{Count: 5, LastUsed: time.Now()}
	if r.Score() <= 0 {
		t.Errorf("Score() = %f, want > 0", r.Score())
	}
}

func TestAgentUsageRecord_Score_ZeroCountValid(t *testing.T) {
	r := AgentUsageRecord{Count: 0, LastUsed: time.Now()}
	s := r.Score()
	if math.IsNaN(s) || math.IsInf(s, 0) {
		t.Errorf("Score() with Count=0 = %f, want finite non-NaN value", s)
	}
}

func makeTeamWithStatuses(statuses ...SessionStatus) *Team {
	sessions := make([]*Session, len(statuses))
	for i, s := range statuses {
		sessions[i] = &Session{Status: s}
	}
	return &Team{Sessions: sessions}
}

func TestTeamStatus_AllDead(t *testing.T) {
	team := makeTeamWithStatuses(StatusDead, StatusDead)
	if got := team.TeamStatus(); got != StatusDead {
		t.Errorf("TeamStatus() = %q, want %q", got, StatusDead)
	}
}

func TestTeamStatus_AllIdle(t *testing.T) {
	team := makeTeamWithStatuses(StatusIdle, StatusIdle)
	if got := team.TeamStatus(); got != StatusIdle {
		t.Errorf("TeamStatus() = %q, want %q", got, StatusIdle)
	}
}

func TestTeamStatus_HasRunning(t *testing.T) {
	team := makeTeamWithStatuses(StatusIdle, StatusRunning)
	if got := team.TeamStatus(); got != StatusRunning {
		t.Errorf("TeamStatus() = %q, want %q", got, StatusRunning)
	}
}

func TestTeamStatus_HasWaiting(t *testing.T) {
	team := makeTeamWithStatuses(StatusIdle, StatusWaiting)
	if got := team.TeamStatus(); got != StatusWaiting {
		t.Errorf("TeamStatus() = %q, want %q", got, StatusWaiting)
	}
}

func TestTeamStatus_WaitingBeatsRunning(t *testing.T) {
	team := makeTeamWithStatuses(StatusRunning, StatusWaiting)
	if got := team.TeamStatus(); got != StatusWaiting {
		t.Errorf("TeamStatus() = %q, want %q (waiting should beat running)", got, StatusWaiting)
	}
}

func TestTeamStatus_EmptySessions(t *testing.T) {
	team := &Team{Sessions: []*Session{}}
	// Empty team: allDead starts true and no session changes it → StatusDead
	if got := team.TeamStatus(); got != StatusDead {
		t.Errorf("TeamStatus() on empty team = %q, want %q", got, StatusDead)
	}
}

func makeAppStateWithSessions() *AppState {
	return &AppState{
		ActiveProjectID: "p1",
		Projects: []*Project{
			{
				ID:   "p1",
				Name: "proj1",
				Sessions: []*Session{
					{ID: "s1", ProjectID: "p1"},
					{ID: "s2", ProjectID: "p1"},
				},
				Teams: []*Team{
					{
						ID: "t1",
						Sessions: []*Session{
							{ID: "s3", ProjectID: "p1", TeamID: "t1"},
						},
					},
				},
			},
		},
	}
}

func TestActiveSession_Found(t *testing.T) {
	s := makeAppStateWithSessions()
	s.ActiveSessionID = "s2"
	sess := s.ActiveSession()
	if sess == nil {
		t.Fatal("ActiveSession() returned nil, want session s2")
	}
	if sess.ID != "s2" {
		t.Errorf("ActiveSession().ID = %q, want %q", sess.ID, "s2")
	}
}

func TestActiveSession_InTeam(t *testing.T) {
	s := makeAppStateWithSessions()
	s.ActiveSessionID = "s3"
	sess := s.ActiveSession()
	if sess == nil {
		t.Fatal("ActiveSession() returned nil for team session s3")
	}
	if sess.ID != "s3" {
		t.Errorf("ActiveSession().ID = %q, want %q", sess.ID, "s3")
	}
}

func TestActiveSession_NotFound(t *testing.T) {
	s := makeAppStateWithSessions()
	s.ActiveSessionID = "nonexistent"
	if got := s.ActiveSession(); got != nil {
		t.Errorf("ActiveSession() = %+v, want nil for unknown ID", got)
	}
}

func TestActiveSession_EmptyID(t *testing.T) {
	s := makeAppStateWithSessions()
	s.ActiveSessionID = ""
	if got := s.ActiveSession(); got != nil {
		t.Errorf("ActiveSession() = %+v, want nil for empty ID", got)
	}
}

func TestActiveProject_Found(t *testing.T) {
	s := makeAppStateWithSessions()
	s.ActiveProjectID = "p1"
	proj := s.ActiveProject()
	if proj == nil {
		t.Fatal("ActiveProject() returned nil")
	}
	if proj.ID != "p1" {
		t.Errorf("ActiveProject().ID = %q, want %q", proj.ID, "p1")
	}
}

func TestActiveProject_NotFound(t *testing.T) {
	s := makeAppStateWithSessions()
	s.ActiveProjectID = "nope"
	if got := s.ActiveProject(); got != nil {
		t.Errorf("ActiveProject() = %+v, want nil", got)
	}
}
