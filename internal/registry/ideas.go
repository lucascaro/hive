package registry

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/wire"
)

// Ideas are captured notes owned by a project: something the user
// noticed while working somewhere else and wants to act on later,
// possibly by starting a session from it. They are registry state
// like sessions and projects — same atomic writer, same
// subscribe/broadcast shape — but with no display order of their own,
// so they persist as flat files and sort newest-first on read.

var (
	// ErrIdeaNotFound is returned when an idea ID isn't known.
	ErrIdeaNotFound = errors.New("registry: idea not found")
	// ErrIdeaTooLong is returned when an idea's text exceeds
	// wire.MaxIdeaText. Rejected, never truncated.
	ErrIdeaTooLong = errors.New("registry: idea text too long")
	// ErrIdeaBadKind / ErrIdeaBadStatus are returned for values
	// outside the closed sets in wire.
	ErrIdeaBadKind   = errors.New("registry: unknown idea kind")
	ErrIdeaBadStatus = errors.New("registry: unknown idea status")
	// ErrProjectHasIdeas is returned by KillProject when the project
	// still holds ideas that are not done. Overridable with the
	// deleteIdeas force flag once the user has confirmed — the same
	// refuse-then-force contract as ErrWorktreeDirty.
	ErrProjectHasIdeas = errors.New("registry: project has open ideas")
)

// IdeaSpec describes an idea to file. Exactly one of ProjectID or
// SessionID must resolve to a known project; when ProjectID is empty
// it is resolved from the live entry for SessionID, so an idea filed
// after a session was reassigned lands in the project the session is
// in now.
type IdeaSpec struct {
	ProjectID string
	SessionID string
	Kind      string
	Text      string
}

// ideaInfo converts the persisted record to its wire shape. ExternalRef
// is deliberately dropped — see IdeaFile.
func ideaInfo(f *IdeaFile) wire.IdeaInfo {
	return wire.IdeaInfo{
		ID:              f.ID,
		ProjectID:       f.ProjectID,
		Kind:            f.Kind,
		Text:            f.Text,
		Status:          f.Status,
		Created:         f.Created.UTC().Format(time.RFC3339),
		Updated:         f.Updated.UTC().Format(time.RFC3339),
		SourceSessionID: f.SourceSessionID,
		SessionID:       f.SessionID,
	}
}

func ideaPath(stateDir, id string) string {
	return filepath.Join(IdeasDir(stateDir), id+".json")
}

// AddIdea files a new idea and persists it before announcing it.
func (r *Registry) AddIdea(spec IdeaSpec) (wire.IdeaInfo, error) {
	text := strings.TrimSpace(spec.Text)
	if text == "" {
		return wire.IdeaInfo{}, errors.New("registry: idea text is empty")
	}
	if len(text) > wire.MaxIdeaText {
		return wire.IdeaInfo{}, fmt.Errorf("%w: %d bytes, limit %d",
			ErrIdeaTooLong, len(text), wire.MaxIdeaText)
	}
	kind := spec.Kind
	if kind == "" {
		kind = wire.IdeaKindIdea
	}
	if !wire.IdeaKinds[kind] {
		return wire.IdeaInfo{}, fmt.Errorf("%w: %q", ErrIdeaBadKind, kind)
	}

	r.mu.Lock()
	projectID := spec.ProjectID
	if projectID == "" && spec.SessionID != "" {
		if e := r.entries[spec.SessionID]; e != nil {
			projectID = e.ProjectID
		}
	}
	if projectID == "" {
		// A session whose project cannot be resolved (unknown id, or
		// an entry still carrying the pre-projects empty string) files
		// into the default project rather than losing the note.
		projectID = r.defaultProjectIDLocked()
	}
	if _, ok := r.projects[projectID]; !ok {
		r.mu.Unlock()
		return wire.IdeaInfo{}, ErrProjectNotFound
	}
	now := time.Now().UTC()
	f := &IdeaFile{
		ID:              uuid.NewString(),
		ProjectID:       projectID,
		Kind:            kind,
		Text:            text,
		Status:          wire.IdeaStatusOpen,
		Created:         now,
		Updated:         now,
		SourceSessionID: spec.SessionID,
	}
	if err := writeJSON(ideaPath(r.stateDir, f.ID), f); err != nil {
		r.mu.Unlock()
		return wire.IdeaInfo{}, err
	}
	r.ideas[f.ID] = f
	info := ideaInfo(f)
	r.mu.Unlock()

	r.broadcastIdea(wire.IdeaEventAdded, info)
	return info, nil
}

// UpdateIdea applies a partial patch. Nil fields are left alone,
// matching UpdateProject's pointer-per-field shape.
func (r *Registry) UpdateIdea(req wire.UpdateIdeaReq) (wire.IdeaInfo, error) {
	if req.Text != nil {
		text := strings.TrimSpace(*req.Text)
		if len(text) > wire.MaxIdeaText {
			return wire.IdeaInfo{}, fmt.Errorf("%w: %d bytes, limit %d",
				ErrIdeaTooLong, len(text), wire.MaxIdeaText)
		}
		req.Text = &text
	}
	if req.Status != nil && !wire.IdeaStatuses[*req.Status] {
		return wire.IdeaInfo{}, fmt.Errorf("%w: %q", ErrIdeaBadStatus, *req.Status)
	}

	r.mu.Lock()
	f, ok := r.ideas[req.ID]
	if !ok {
		r.mu.Unlock()
		return wire.IdeaInfo{}, ErrIdeaNotFound
	}
	// Patch a copy: a failed write must not leave the in-memory record
	// ahead of the file on disk.
	next := *f
	if req.Text != nil {
		if *req.Text == "" {
			r.mu.Unlock()
			return wire.IdeaInfo{}, errors.New("registry: idea text is empty")
		}
		next.Text = *req.Text
	}
	if req.Status != nil {
		next.Status = *req.Status
	}
	if req.SessionID != nil {
		next.SessionID = *req.SessionID
	}
	next.Updated = time.Now().UTC()
	if err := writeJSON(ideaPath(r.stateDir, next.ID), &next); err != nil {
		r.mu.Unlock()
		return wire.IdeaInfo{}, err
	}
	*f = next
	info := ideaInfo(f)
	r.mu.Unlock()

	r.broadcastIdea(wire.IdeaEventUpdated, info)
	return info, nil
}

// RemoveIdea deletes one idea.
func (r *Registry) RemoveIdea(id string) error {
	r.mu.Lock()
	f, ok := r.ideas[id]
	if !ok {
		r.mu.Unlock()
		return ErrIdeaNotFound
	}
	info := ideaInfo(f)
	delete(r.ideas, id)
	r.mu.Unlock()

	if err := os.Remove(ideaPath(r.stateDir, id)); err != nil && !os.IsNotExist(err) {
		log.Printf("registry: remove idea %s: %v", id, err)
	}
	r.broadcastIdea(wire.IdeaEventRemoved, info)
	return nil
}

// ListIdeas returns the ideas of one project, or every idea when
// projectID is empty. Newest first: Created is the only order there
// is, which is why ideas need no index file.
func (r *Registry) ListIdeas(projectID string) []wire.IdeaInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]wire.IdeaInfo, 0, len(r.ideas))
	for _, f := range r.ideas {
		if projectID != "" && f.ProjectID != projectID {
			continue
		}
		out = append(out, ideaInfo(f))
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Created != out[j].Created {
			return out[i].Created > out[j].Created
		}
		// Same-second creates need a stable tiebreak, or the list
		// reshuffles between two renders of identical state.
		return out[i].ID < out[j].ID
	})
	return out
}

// openIdeasLocked counts the project's ideas that are not done. Must
// be called with r.mu held.
func (r *Registry) openIdeasLocked(projectID string) int {
	n := 0
	for _, f := range r.ideas {
		if f.ProjectID == projectID && f.Status != wire.IdeaStatusDone {
			n++
		}
	}
	return n
}

// removeProjectIdeasLocked drops every idea belonging to a project
// from memory and returns what was removed, so the caller can delete
// the files and broadcast outside the lock. Must be called with r.mu
// held.
func (r *Registry) removeProjectIdeasLocked(projectID string) []wire.IdeaInfo {
	var removed []wire.IdeaInfo
	for id, f := range r.ideas {
		if f.ProjectID != projectID {
			continue
		}
		removed = append(removed, ideaInfo(f))
		delete(r.ideas, id)
	}
	return removed
}

// SubscribeIdeas returns a channel that receives IdeaEvent. Slow
// consumers are dropped — listeners must drain promptly.
func (r *Registry) SubscribeIdeas() (IdeaListener, func()) {
	// 64 for the same reason as Subscribe: a project delete
	// broadcasts one event per idea it destroys.
	ch := make(IdeaListener, 64)
	r.mu.Lock()
	if r.ideaListeners == nil {
		// Post-Close subscribe; see the matching guard in Subscribe.
		r.mu.Unlock()
		close(ch)
		return ch, func() {}
	}
	r.ideaListeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.ideaListeners[ch]; ok {
			delete(r.ideaListeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

func (r *Registry) broadcastIdea(kind string, info wire.IdeaInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := wire.IdeaEvent{Kind: kind, Idea: info}
	for ch := range r.ideaListeners {
		select {
		case ch <- ev:
		default:
			// Same contract as broadcastLocked: drops must be loud.
			log.Printf("registry: dropping slow idea-event listener (buffer %d full, %d listeners); client is desynced until it resubscribes",
				cap(ch), len(r.ideaListeners))
			delete(r.ideaListeners, ch)
			close(ch)
		}
	}
}

// loadIdeas reads every ideas/<id>.json. A malformed file is logged
// and skipped, never deleted: the user's captured note is the thing
// this whole feature exists to not lose, and a parse failure is at
// least as likely to be our bug as their corruption.
func (r *Registry) loadIdeas() error {
	dir := IdeasDir(r.stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, d := range entries {
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".json") {
			continue
		}
		var f IdeaFile
		path := filepath.Join(dir, d.Name())
		if err := readJSON(path, &f); err != nil {
			log.Printf("registry: skipping malformed idea %s: %v", path, err)
			continue
		}
		if f.ID == "" {
			log.Printf("registry: skipping idea %s: no id", path)
			continue
		}
		if !wire.IdeaKinds[f.Kind] {
			f.Kind = wire.IdeaKindIdea
		}
		if !wire.IdeaStatuses[f.Status] {
			f.Status = wire.IdeaStatusOpen
		}
		// An idea whose project is gone (KillProject persisted the
		// project index before unlinking the idea files, then crashed)
		// would reload forever, unreachable through any project
		// filter. Reattach it to the default project rather than drop
		// it — losing the note is the one outcome this feature cannot
		// afford. With no projects yet there is nothing to reattach
		// to; keep it as-is and let EnsureDefaultProject's first run
		// sort it out on the next boot.
		if _, ok := r.projects[f.ProjectID]; !ok {
			if def := r.defaultProjectIDLocked(); def != "" {
				log.Printf("registry: idea %s names unknown project %q; reattaching to %s", f.ID, f.ProjectID, def)
				f.ProjectID = def
				if err := writeJSON(path, &f); err != nil {
					log.Printf("registry: persist reattached idea %s: %v", f.ID, err)
				}
			} else {
				log.Printf("registry: idea %s names unknown project %q and no project exists yet; keeping as-is", f.ID, f.ProjectID)
			}
		}
		r.ideas[f.ID] = &f
	}
	return nil
}
