package registry

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

// ErrProjectNotFound is returned when a project ID isn't known.
var ErrProjectNotFound = errors.New("registry: project not found")

// ListProjects returns a snapshot of all projects in display order.
func (r *Registry) ListProjects() []wire.ProjectInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]wire.ProjectInfo, 0, len(r.projectOrder))
	for _, id := range r.projectOrder {
		if p := r.projects[id]; p != nil {
			out = append(out, p.Info())
		}
	}
	return out
}

// GetProject returns the project with the given ID, or nil.
func (r *Registry) GetProject(id string) *Project {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.projects[id]
}

// EnsureDefaultProject creates a project named "default" rooted at
// the given cwd if no projects exist on disk. Idempotent. Used by the
// daemon at startup so a fresh install always has a project to host
// new sessions.
func (r *Registry) EnsureDefaultProject(cwd string) (*Project, error) {
	r.mu.Lock()
	if len(r.projectOrder) > 0 {
		first := r.projects[r.projectOrder[0]]
		r.mu.Unlock()
		return first, nil
	}
	r.mu.Unlock()
	return r.CreateProject(wire.CreateProjectReq{Name: "default", Cwd: cwd})
}

// ReclaimOrphanWorktrees scans every project's <projectCwd>/.worktrees
// directory and runs worktree.Cleanup on any subdirectory whose path
// is not claimed by some live registry entry. Idempotent. Run once at
// daemon startup so a SIGKILL'd previous process doesn't leak.
func (r *Registry) ReclaimOrphanWorktrees() {
	r.mu.Lock()
	claimed := make(map[string]bool, len(r.entries))
	for _, e := range r.entries {
		if e.WorktreePath != "" {
			claimed[e.WorktreePath] = true
		}
	}
	projects := make([]*Project, 0, len(r.projects))
	for _, p := range r.projects {
		projects = append(projects, p)
	}
	r.mu.Unlock()

	for _, p := range projects {
		if p.Cwd == "" || !worktree.IsGitRepo(p.Cwd) {
			continue
		}
		root, err := worktree.Root(p.Cwd)
		if err != nil {
			continue
		}
		wtDir := filepath.Join(root, ".worktrees")
		entries, err := os.ReadDir(wtDir)
		if err != nil {
			continue
		}
		for _, d := range entries {
			if !d.IsDir() {
				continue
			}
			path := filepath.Join(wtDir, d.Name())
			if claimed[path] {
				continue
			}
			log.Printf("registry: reclaiming orphan worktree %s", path)
			if err := worktree.Cleanup(root, path); err != nil {
				log.Printf("registry: orphan cleanup failed for %s: %v", path, err)
			}
		}
	}
}

// MigrateOrphanSessions assigns any session without a ProjectID to
// the default (first) project. Run on startup so users coming from
// pre-Phase-4 state don't see their sessions vanish.
func (r *Registry) MigrateOrphanSessions() {
	r.mu.Lock()
	defer r.mu.Unlock()
	defID := r.defaultProjectIDLocked()
	if defID == "" {
		return
	}
	dirty := false
	for _, e := range r.entries {
		if e.ProjectID == "" {
			e.ProjectID = defID
			_ = r.persistEntryLocked(e)
			dirty = true
		}
	}
	_ = dirty // currently no aggregate side-effect, but keep for clarity
}

// defaultProjectIDLocked must be called with r.mu held. Returns the
// first project's ID, or "" if none exist (caller should ensure a
// default exists first).
func (r *Registry) defaultProjectIDLocked() string {
	if len(r.projectOrder) == 0 {
		return ""
	}
	return r.projectOrder[0]
}

// CreateProject adds a new project and persists it.
func (r *Registry) CreateProject(req wire.CreateProjectReq) (*Project, error) {
	r.mu.Lock()
	id := uuid.NewString()
	name := req.Name
	if name == "" {
		name = fmt.Sprintf("project %d", len(r.projectOrder)+1)
	}
	color := req.Color
	if color == "" {
		color = pickColor(len(r.projectOrder))
	}
	p := &Project{
		ID: id, Name: name, Color: color, Cwd: req.Cwd,
		Order: len(r.projectOrder), Created: time.Now().UTC(),
	}
	r.projects[id] = p
	r.projectOrder = append(r.projectOrder, id)
	if err := r.persistProjectLocked(p); err != nil {
		delete(r.projects, id)
		r.projectOrder = r.projectOrder[:len(r.projectOrder)-1]
		r.mu.Unlock()
		return nil, err
	}
	if err := r.persistProjectIndexLocked(); err != nil {
		delete(r.projects, id)
		r.projectOrder = r.projectOrder[:len(r.projectOrder)-1]
		r.mu.Unlock()
		return nil, err
	}
	info := p.Info()
	r.mu.Unlock()
	r.broadcastProject(wire.ProjectEventAdded, info)
	return p, nil
}

// KillProject removes a project. If killSessions is true the project's
// sessions are terminated; otherwise they're reassigned to the
// default project (which is never the project being killed unless
// it's the only one — in that case we refuse).
func (r *Registry) KillProject(id string, killSessions bool) error {
	r.mu.Lock()
	p, ok := r.projects[id]
	if !ok {
		r.mu.Unlock()
		return ErrProjectNotFound
	}
	if len(r.projectOrder) == 1 {
		r.mu.Unlock()
		return errors.New("registry: refusing to remove the only project")
	}

	// Pick the target for reassignment: the first project that isn't
	// the one being killed.
	var targetID string
	for _, oid := range r.projectOrder {
		if oid != id {
			targetID = oid
			break
		}
	}

	// Collect sessions in this project.
	affected := make([]*Entry, 0)
	for _, sid := range r.order {
		if e := r.entries[sid]; e != nil && e.ProjectID == id {
			affected = append(affected, e)
		}
	}

	// Reassign or kill each affected session.
	if !killSessions {
		for _, e := range affected {
			e.ProjectID = targetID
			_ = r.persistEntryLocked(e)
		}
	}

	delete(r.projects, id)
	for i, pid := range r.projectOrder {
		if pid == id {
			r.projectOrder = append(r.projectOrder[:i], r.projectOrder[i+1:]...)
			break
		}
	}
	r.renumberProjectsLocked()
	_ = r.persistProjectIndexLocked()

	dir := filepath.Join(ProjectsDir(r.stateDir), id)
	info := p.Info()
	r.mu.Unlock()

	_ = os.RemoveAll(dir)

	if killSessions {
		for _, e := range affected {
			_ = r.Kill(e.ID, true)
		}
	} else {
		for _, e := range affected {
			r.broadcast(wire.SessionEventUpdated, e.Info())
		}
	}
	r.broadcastProject(wire.ProjectEventRemoved, info)
	return nil
}

// UpdateProject mutates project metadata.
func (r *Registry) UpdateProject(req wire.UpdateProjectReq) (*Project, error) {
	r.mu.Lock()
	p, ok := r.projects[req.ProjectID]
	if !ok {
		r.mu.Unlock()
		return nil, ErrProjectNotFound
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Color != nil {
		p.Color = *req.Color
	}
	if req.Cwd != nil {
		p.Cwd = *req.Cwd
	}
	if req.Order != nil {
		r.moveProjectLocked(p.ID, *req.Order)
	}
	if err := r.persistProjectLocked(p); err != nil {
		r.mu.Unlock()
		return p, err
	}
	if err := r.persistProjectIndexLocked(); err != nil {
		r.mu.Unlock()
		return p, err
	}
	info := p.Info()
	r.mu.Unlock()
	r.broadcastProject(wire.ProjectEventUpdated, info)
	return p, nil
}

func (r *Registry) moveProjectLocked(id string, newOrder int) {
	cur := -1
	for i, s := range r.projectOrder {
		if s == id {
			cur = i
			break
		}
	}
	if cur < 0 {
		return
	}
	r.projectOrder = append(r.projectOrder[:cur], r.projectOrder[cur+1:]...)
	if newOrder < 0 {
		newOrder = 0
	}
	if newOrder > len(r.projectOrder) {
		newOrder = len(r.projectOrder)
	}
	r.projectOrder = append(r.projectOrder[:newOrder], append([]string{id}, r.projectOrder[newOrder:]...)...)
	r.renumberProjectsLocked()
}

func (r *Registry) renumberProjectsLocked() {
	for i, id := range r.projectOrder {
		if p := r.projects[id]; p != nil {
			p.Order = i
			_ = r.persistProjectLocked(p)
		}
	}
}

// SubscribeProjects returns a channel that receives ProjectEvent.
// Slow consumers are dropped — listeners must drain promptly.
func (r *Registry) SubscribeProjects() (ProjectListener, func()) {
	ch := make(ProjectListener, 16)
	r.mu.Lock()
	r.projectListeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.projectListeners[ch]; ok {
			delete(r.projectListeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) broadcastProject(kind string, info wire.ProjectInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := wire.ProjectEvent{Kind: kind, Project: info}
	for ch := range r.projectListeners {
		select {
		case ch <- ev:
		default:
			delete(r.projectListeners, ch)
			close(ch)
		}
	}
}
