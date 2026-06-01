package client

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

var twoSessions = []wire.SessionInfo{
	{ID: "aaa", Name: "api", Alive: true},
	{ID: "bbb", Name: "web", Alive: true},
}

func TestResolveSession_ExplicitIDWins(t *testing.T) {
	id, err := ResolveSession(twoSessions, "bbb")
	if err != nil || id != "bbb" {
		t.Fatalf("got (%q,%v), want (bbb,nil)", id, err)
	}
}

func TestResolveSession_UnknownIDErrors(t *testing.T) {
	_, err := ResolveSession(twoSessions, "zzz")
	if err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("want 'no session' error, got %v", err)
	}
}

func TestResolveSession_NoArgSingleSession(t *testing.T) {
	id, err := ResolveSession(twoSessions[:1], "")
	if err != nil || id != "aaa" {
		t.Fatalf("got (%q,%v), want (aaa,nil)", id, err)
	}
}

func TestResolveSession_NoArgMultipleErrors(t *testing.T) {
	_, err := ResolveSession(twoSessions, "")
	if err == nil || !strings.Contains(err.Error(), "specify a session id") {
		t.Fatalf("want 'specify a session id' error, got %v", err)
	}
}

func TestResolveSession_NoneErrors(t *testing.T) {
	_, err := ResolveSession(nil, "")
	if err == nil || !strings.Contains(err.Error(), "no sessions") {
		t.Fatalf("want 'no sessions' error, got %v", err)
	}
}
