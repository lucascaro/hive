package registry

import (
	"runtime"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

func skipNonPosix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("project tests start sessions; require POSIX shell")
	}
}

func TestEnsureDefaultProject(t *testing.T) {
	r := freshRegistry(t)
	p, err := r.EnsureDefaultProject("/tmp")
	if err != nil {
		t.Fatalf("EnsureDefault: %v", err)
	}
	if p.Name != "default" || p.Cwd != "/tmp" {
		t.Errorf("default project: %+v", p)
	}
	// Idempotent.
	p2, err := r.EnsureDefaultProject("/var")
	if err != nil {
		t.Fatalf("EnsureDefault again: %v", err)
	}
	if p2.ID != p.ID {
		t.Errorf("EnsureDefault should be idempotent")
	}
	// Cwd should not have changed.
	if p2.Cwd != "/tmp" {
		t.Errorf("default project cwd was overwritten: %q", p2.Cwd)
	}
}

func TestSessionInheritsProjectCwd(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "p", Color: "#abc", Cwd: "/tmp"})
	e, err := r.Create(wire.CreateSpec{ProjectID: p.ID, Shell: "/bin/bash"})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	if e.ProjectID != p.ID {
		t.Errorf("session.ProjectID = %s, want %s", e.ProjectID, p.ID)
	}
	// We can't easily read the session's Cmd.Dir back; behavior is
	// covered by the daemon E2E test (which runs `pwd` in the new
	// session). Just assert the session started successfully.
}

func TestKillProjectReassign(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	def, _ := r.EnsureDefaultProject("/tmp")
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "extra"})
	// Two sessions in p.
	a, _ := r.Create(wire.CreateSpec{ProjectID: p.ID, Shell: "/bin/bash"})
	b, _ := r.Create(wire.CreateSpec{ProjectID: p.ID, Shell: "/bin/bash"})
	if err := r.KillProject(p.ID, false); err != nil {
		t.Fatalf("KillProject reassign: %v", err)
	}
	for _, e := range []*Entry{r.Get(a.ID), r.Get(b.ID)} {
		if e == nil {
			t.Fatalf("session missing after reassign")
		}
		if e.ProjectID != def.ID {
			t.Errorf("session not reassigned: project_id=%s want %s", e.ProjectID, def.ID)
		}
	}
}

func TestKillProjectKillSessions(t *testing.T) {
	skipNonPosix(t)
	r := freshRegistry(t)
	_, _ = r.EnsureDefaultProject("/tmp")
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "doomed"})
	a, _ := r.Create(wire.CreateSpec{ProjectID: p.ID, Shell: "/bin/bash"})
	if err := r.KillProject(p.ID, true); err != nil {
		t.Fatalf("KillProject killSessions: %v", err)
	}
	if r.Get(a.ID) != nil {
		t.Errorf("session should be gone after KillProject(killSessions=true)")
	}
}

func TestUpdateProject(t *testing.T) {
	r := freshRegistry(t)
	p, _ := r.CreateProject(wire.CreateProjectReq{Name: "x", Cwd: "/old"})
	newName := "renamed"
	newCwd := "/new"
	_, err := r.UpdateProject(wire.UpdateProjectReq{
		ProjectID: p.ID,
		Name:      &newName,
		Cwd:       &newCwd,
	})
	if err != nil {
		t.Fatalf("UpdateProject: %v", err)
	}
	got := r.GetProject(p.ID)
	if got.Name != newName || got.Cwd != newCwd {
		t.Errorf("after update: %+v", got)
	}
}

func TestProjectsPersistAcrossOpen(t *testing.T) {
	dir := t.TempDir()
	r1, _ := Open(dir)
	a, _ := r1.CreateProject(wire.CreateProjectReq{Name: "alpha", Color: "#1", Cwd: "/a"})
	b, _ := r1.CreateProject(wire.CreateProjectReq{Name: "beta", Color: "#2", Cwd: "/b"})
	_ = a
	_ = b
	_ = r1.Close()

	r2, err := Open(dir)
	if err != nil {
		t.Fatalf("re-Open: %v", err)
	}
	defer r2.Close()

	got := r2.ListProjects()
	if len(got) != 2 {
		t.Fatalf("after reopen: %d projects", len(got))
	}
	if got[0].Name != "alpha" || got[0].Cwd != "/a" {
		t.Errorf("first: %+v", got[0])
	}
	if got[1].Name != "beta" || got[1].Cwd != "/b" {
		t.Errorf("second: %+v", got[1])
	}
}
