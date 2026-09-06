package registry

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// ideaRegistry is a registry with one project, which is what every
// idea needs to belong to.
func ideaRegistry(t *testing.T) (*Registry, *Project) {
	t.Helper()
	r := freshRegistry(t)
	p, err := r.EnsureDefaultProject(t.TempDir())
	if err != nil {
		t.Fatalf("EnsureDefaultProject: %v", err)
	}
	return r, p
}

func TestAddIdeaPersistsAtomically(t *testing.T) {
	r, p := ideaRegistry(t)
	info, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Kind: wire.IdeaKindBug, Text: "  sidebar is 1px off  "})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	if info.Text != "sidebar is 1px off" {
		t.Errorf("text = %q, want it trimmed", info.Text)
	}
	if info.Status != wire.IdeaStatusOpen || info.Kind != wire.IdeaKindBug {
		t.Errorf("status/kind = %q/%q", info.Status, info.Kind)
	}
	if info.Created == "" || info.Updated == "" {
		t.Errorf("timestamps not set: %+v", info)
	}
	path := filepath.Join(IdeasDir(r.stateDir), info.ID+".json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("idea file not written: %v", err)
	}
	// writeAtomic's temp file must not survive the rename.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Errorf("temp file left behind: %v", err)
	}
}

func TestIdeasSurviveReload(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p, _ := r.EnsureDefaultProject(t.TempDir())
	info, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "survive me"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	_ = r.Close()

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer r2.Close()
	got := r2.ListIdeas("")
	if len(got) != 1 || got[0].ID != info.ID || got[0].Text != "survive me" {
		t.Fatalf("after reload: %+v", got)
	}
}

func TestMalformedIdeaSkipped(t *testing.T) {
	dir := t.TempDir()
	r, err := Open(dir)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	p, _ := r.EnsureDefaultProject(t.TempDir())
	good, _ := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "good one"})
	_ = r.Close()

	bad := filepath.Join(IdeasDir(dir), "broken.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen with malformed idea: %v", err)
	}
	defer r2.Close()
	got := r2.ListIdeas("")
	if len(got) != 1 || got[0].ID != good.ID {
		t.Fatalf("malformed idea not skipped cleanly: %+v", got)
	}
	// Skipped, never deleted — the note is the thing we must not lose.
	if _, err := os.Stat(bad); err != nil {
		t.Errorf("malformed idea file was removed: %v", err)
	}
}

func TestAddIdeaRejectsOversizeText(t *testing.T) {
	r, p := ideaRegistry(t)
	_, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: strings.Repeat("x", wire.MaxIdeaText+1)})
	if !errors.Is(err, ErrIdeaTooLong) {
		t.Fatalf("err = %v, want ErrIdeaTooLong", err)
	}
	if got := r.ListIdeas(""); len(got) != 0 {
		t.Errorf("oversize idea was stored: %+v", got)
	}
	entries, _ := os.ReadDir(IdeasDir(r.stateDir))
	if len(entries) != 0 {
		t.Errorf("oversize idea wrote a file: %v", entries)
	}
}

func TestAddIdeaRejectsUnknownKind(t *testing.T) {
	r, p := ideaRegistry(t)
	if _, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Kind: "rant", Text: "hi"}); !errors.Is(err, ErrIdeaBadKind) {
		t.Fatalf("err = %v, want ErrIdeaBadKind", err)
	}
}

func TestUpdateIdeaRejectsUnknownStatus(t *testing.T) {
	r, p := ideaRegistry(t)
	info, _ := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "hi"})
	bad := "shipped"
	if _, err := r.UpdateIdea(wire.UpdateIdeaReq{ID: info.ID, Status: &bad}); !errors.Is(err, ErrIdeaBadStatus) {
		t.Fatalf("err = %v, want ErrIdeaBadStatus", err)
	}
	if got := r.ListIdeas("")[0]; got.Status != wire.IdeaStatusOpen {
		t.Errorf("status changed despite refusal: %q", got.Status)
	}
}

func TestAddIdeaUnknownProject(t *testing.T) {
	r, _ := ideaRegistry(t)
	if _, err := r.AddIdea(IdeaSpec{ProjectID: "nope", Text: "hi"}); !errors.Is(err, ErrProjectNotFound) {
		t.Fatalf("err = %v, want ErrProjectNotFound", err)
	}
}

func TestUpdateIdeaUnknownID(t *testing.T) {
	r, _ := ideaRegistry(t)
	if _, err := r.UpdateIdea(wire.UpdateIdeaReq{ID: "nope"}); !errors.Is(err, ErrIdeaNotFound) {
		t.Fatalf("err = %v, want ErrIdeaNotFound", err)
	}
}

func TestRemoveIdeaUnknownID(t *testing.T) {
	r, _ := ideaRegistry(t)
	if err := r.RemoveIdea("nope"); !errors.Is(err, ErrIdeaNotFound) {
		t.Fatalf("err = %v, want ErrIdeaNotFound", err)
	}
}

func TestListIdeasEmptyProjectReturnsAll(t *testing.T) {
	r, p := ideaRegistry(t)
	other, _ := r.CreateProject(wire.CreateProjectReq{Name: "other"})
	a, _ := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "in default"})
	b, _ := r.AddIdea(IdeaSpec{ProjectID: other.ID, Text: "in other"})

	if got := r.ListIdeas(""); len(got) != 2 {
		t.Fatalf("ListIdeas(\"\") = %d ideas, want 2", len(got))
	}
	got := r.ListIdeas(other.ID)
	if len(got) != 1 || got[0].ID != b.ID {
		t.Fatalf("ListIdeas(other) = %+v", got)
	}
	if got := r.ListIdeas(p.ID); len(got) != 1 || got[0].ID != a.ID {
		t.Fatalf("ListIdeas(default) = %+v", got)
	}
}

func TestAddIdeaResolvesProjectFromSession(t *testing.T) {
	skipNonPosix(t)
	r, def := ideaRegistry(t)
	other, _ := r.CreateProject(wire.CreateProjectReq{Name: "other"})
	e, err := r.Create(context.Background(), wire.CreateSpec{ProjectID: other.ID, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	first, err := r.AddIdea(IdeaSpec{SessionID: e.ID, Text: "filed from the session"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	if first.ProjectID != other.ID {
		t.Errorf("project = %s, want %s", first.ProjectID, other.ID)
	}
	if first.SourceSessionID != e.ID {
		t.Errorf("source session = %q, want %q", first.SourceSessionID, e.ID)
	}

	// The project is read from the live entry at file time, never
	// captured when the session spawned — which is why no
	// HIVE_PROJECT_ID goes into the session's environment: it would be
	// stale from the first reassignment on. Deleting the project
	// without killing its sessions is that reassignment.
	if err := r.KillProject(other.ID, false, true); err != nil {
		t.Fatalf("KillProject(other): %v", err)
	}
	second, err := r.AddIdea(IdeaSpec{SessionID: e.ID, Text: "filed after reassign"})
	if err != nil {
		t.Fatalf("AddIdea after reassign: %v", err)
	}
	if second.ProjectID != def.ID {
		t.Errorf("after reassign project = %s, want %s", second.ProjectID, def.ID)
	}
}

func TestKillProjectRefusesWithOpenIdeas(t *testing.T) {
	r, def := ideaRegistry(t)
	doomed, _ := r.CreateProject(wire.CreateProjectReq{Name: "doomed"})
	idea, _ := r.AddIdea(IdeaSpec{ProjectID: doomed.ID, Text: "still open"})

	err := r.KillProject(doomed.ID, false, false)
	if !errors.Is(err, ErrProjectHasIdeas) {
		t.Fatalf("err = %v, want ErrProjectHasIdeas", err)
	}
	// The refusal names the count, which is what the confirm shows.
	if !strings.Contains(err.Error(), "1 open") {
		t.Errorf("refusal does not name the count: %v", err)
	}
	if len(r.ListProjects()) != 2 {
		t.Errorf("project was removed despite the refusal")
	}
	if got := r.ListIdeas(doomed.ID); len(got) != 1 || got[0].ID != idea.ID {
		t.Errorf("idea lost on a refused delete: %+v", got)
	}

	// A done idea is not open, so the delete goes through unforced.
	done := wire.IdeaStatusDone
	if _, err := r.UpdateIdea(wire.UpdateIdeaReq{ID: idea.ID, Status: &done}); err != nil {
		t.Fatalf("UpdateIdea: %v", err)
	}
	if err := r.KillProject(doomed.ID, false, false); err != nil {
		t.Fatalf("KillProject with only done ideas: %v", err)
	}
	if got := r.ListIdeas(""); len(got) != 0 {
		t.Errorf("done idea outlived its project: %+v", got)
	}
	_ = def
}

func TestKillProjectDeletesIdeasWithForce(t *testing.T) {
	r, _ := ideaRegistry(t)
	doomed, _ := r.CreateProject(wire.CreateProjectReq{Name: "doomed"})
	idea, _ := r.AddIdea(IdeaSpec{ProjectID: doomed.ID, Text: "goes with the project"})

	listener, unsub := r.SubscribeIdeas()
	defer unsub()

	if err := r.KillProject(doomed.ID, false, true); err != nil {
		t.Fatalf("KillProject(deleteIdeas): %v", err)
	}
	if got := r.ListIdeas(""); len(got) != 0 {
		t.Errorf("ideas survived their project: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(IdeasDir(r.stateDir), idea.ID+".json")); !os.IsNotExist(err) {
		t.Errorf("idea file survived: %v", err)
	}
	select {
	case ev := <-listener:
		if ev.Kind != wire.IdeaEventRemoved || ev.Idea.ID != idea.ID {
			t.Errorf("event = %+v, want removed %s", ev, idea.ID)
		}
	default:
		t.Error("no IDEA_EVENT(removed) broadcast on the cascade")
	}
}

func TestIdeaEventsBroadcast(t *testing.T) {
	r, p := ideaRegistry(t)
	listener, unsub := r.SubscribeIdeas()
	defer unsub()

	info, err := r.AddIdea(IdeaSpec{ProjectID: p.ID, Text: "one"})
	if err != nil {
		t.Fatalf("AddIdea: %v", err)
	}
	started := wire.IdeaStatusStarted
	sid := "sess-1"
	if _, err := r.UpdateIdea(wire.UpdateIdeaReq{ID: info.ID, Status: &started, SessionID: &sid}); err != nil {
		t.Fatalf("UpdateIdea: %v", err)
	}
	if err := r.RemoveIdea(info.ID); err != nil {
		t.Fatalf("RemoveIdea: %v", err)
	}

	for _, want := range []string{wire.IdeaEventAdded, wire.IdeaEventUpdated, wire.IdeaEventRemoved} {
		select {
		case ev := <-listener:
			if ev.Kind != want {
				t.Fatalf("event kind = %q, want %q", ev.Kind, want)
			}
			if want == wire.IdeaEventUpdated && ev.Idea.SessionID != sid {
				t.Errorf("started idea did not carry its session: %+v", ev.Idea)
			}
		default:
			t.Fatalf("missing %s event", want)
		}
	}
}
