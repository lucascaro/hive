// Package registry tracks the daemon's open sessions and their
// user-facing metadata (name, color, order). It owns persistence so
// the daemon's main loop can stay focused on transport.
package registry

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/lucascaro/hive/internal/session"
	"github.com/lucascaro/hive/internal/wire"
)

// ErrNotFound is returned when a session ID isn't known.
var ErrNotFound = errors.New("registry: session not found")

// Entry pairs persisted metadata with the live session. The session is
// nil for entries loaded from disk that haven't been started this run.
type Entry struct {
	ID      string
	Name    string
	Color   string
	Order   int
	Created time.Time
	sess    *session.Session // nil ⇔ not running this lifetime
}

// Alive reports whether this entry has a live session attached.
func (e *Entry) Alive() bool { return e.sess != nil }

// Session returns the live session, or nil.
func (e *Entry) Session() *session.Session { return e.sess }

// Info renders the entry as a wire.SessionInfo for the protocol.
func (e *Entry) Info() wire.SessionInfo {
	return wire.SessionInfo{
		ID:      e.ID,
		Name:    e.Name,
		Color:   e.Color,
		Order:   e.Order,
		Created: e.Created.UTC().Format(time.RFC3339),
		Alive:   e.Alive(),
	}
}

// Listener is a channel that receives SessionEvent notifications.
type Listener chan wire.SessionEvent

// Registry is the daemon-side authoritative store of sessions.
type Registry struct {
	mu       sync.Mutex
	entries  map[string]*Entry
	order    []string
	stateDir string

	// Listeners are notified of every change. Slow listeners are dropped.
	listeners map[Listener]struct{}
}

// Open creates or loads a Registry rooted at stateDir. Existing
// metadata on disk is loaded; live sessions are not auto-started.
func Open(stateDir string) (*Registry, error) {
	if stateDir == "" {
		stateDir = StateDir()
	}
	r := &Registry{
		entries:   make(map[string]*Entry),
		stateDir:  stateDir,
		listeners: make(map[Listener]struct{}),
	}
	if err := r.load(); err != nil {
		return nil, fmt.Errorf("registry: load: %w", err)
	}
	return r, nil
}

// load reads index.json + every session.json under sessions/. Missing
// files are tolerated; corrupt files are skipped with a best-effort
// recovery.
func (r *Registry) load() error {
	dir := SessionsDir(r.stateDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var idx IndexFile
	_ = readJSON(filepath.Join(dir, "index.json"), &idx) // OK if missing

	// Build entries from per-session metadata files. The index gives
	// order; any sessions present on disk but missing from the index
	// are appended to the end.
	seen := make(map[string]bool)
	for _, id := range idx.Order {
		var meta MetaFile
		if err := readJSON(filepath.Join(dir, id, "session.json"), &meta); err != nil {
			continue
		}
		r.entries[meta.ID] = &Entry{
			ID: meta.ID, Name: meta.Name, Color: meta.Color,
			Order: meta.Order, Created: meta.Created,
		}
		r.order = append(r.order, meta.ID)
		seen[meta.ID] = true
	}
	// Catch sessions on disk not in the index.
	if dirs, err := os.ReadDir(dir); err == nil {
		for _, d := range dirs {
			if !d.IsDir() || seen[d.Name()] {
				continue
			}
			var meta MetaFile
			if err := readJSON(filepath.Join(dir, d.Name(), "session.json"), &meta); err != nil {
				continue
			}
			meta.Order = len(r.order)
			r.entries[meta.ID] = &Entry{
				ID: meta.ID, Name: meta.Name, Color: meta.Color,
				Order: meta.Order, Created: meta.Created,
			}
			r.order = append(r.order, meta.ID)
		}
	}
	return nil
}

// List returns a snapshot of all entries in display order.
func (r *Registry) List() []wire.SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]wire.SessionInfo, 0, len(r.order))
	for _, id := range r.order {
		if e := r.entries[id]; e != nil {
			out = append(out, e.Info())
		}
	}
	return out
}

// Get returns the entry for id, or nil.
func (r *Registry) Get(id string) *Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.entries[id]
}

// Create adds a new session and starts it. Metadata persists before
// the session starts so a crash mid-Create still surfaces the entry.
func (r *Registry) Create(spec wire.CreateSpec) (*Entry, error) {
	r.mu.Lock()
	id := uuid.NewString()
	name := spec.Name
	if name == "" {
		name = fmt.Sprintf("session %d", len(r.order)+1)
	}
	color := spec.Color
	if color == "" {
		color = pickColor(len(r.order))
	}
	e := &Entry{
		ID: id, Name: name, Color: color,
		Order: len(r.order), Created: time.Now().UTC(),
	}
	r.entries[id] = e
	r.order = append(r.order, id)
	if err := r.persistEntryLocked(e); err != nil {
		// Roll back the in-memory state so the registry stays consistent.
		delete(r.entries, id)
		r.order = r.order[:len(r.order)-1]
		r.mu.Unlock()
		return nil, err
	}
	if err := r.persistIndexLocked(); err != nil {
		delete(r.entries, id)
		r.order = r.order[:len(r.order)-1]
		r.mu.Unlock()
		return nil, err
	}
	r.mu.Unlock()

	// Start the session outside the lock so the PTY fork doesn't block
	// the registry.
	sess, err := session.Start(session.Options{
		Shell: spec.Shell,
		Cols:  spec.Cols,
		Rows:  spec.Rows,
	})
	if err != nil {
		// Strand the metadata as a dead entry. The user can recreate
		// or kill it.
		r.broadcast(wire.SessionEventAdded, e.Info())
		return e, err
	}
	r.mu.Lock()
	// The session.Session uses its own UUID; we override with the
	// registry id so the registry id is the public identity.
	sess.ID = id
	e.sess = sess
	r.mu.Unlock()
	r.broadcast(wire.SessionEventAdded, e.Info())
	return e, nil
}

// Adopt registers an externally-started session under the given
// metadata. Used by the daemon for its bootstrap session in Phase 2
// transitional code (before the GUI calls CREATE_SESSION explicitly).
func (r *Registry) Adopt(s *session.Session, name, color string) (*Entry, error) {
	r.mu.Lock()
	id := s.ID
	if existing := r.entries[id]; existing != nil {
		existing.sess = s
		r.mu.Unlock()
		r.broadcast(wire.SessionEventUpdated, existing.Info())
		return existing, nil
	}
	if name == "" {
		name = fmt.Sprintf("session %d", len(r.order)+1)
	}
	if color == "" {
		color = pickColor(len(r.order))
	}
	e := &Entry{
		ID: id, Name: name, Color: color,
		Order: len(r.order), Created: time.Now().UTC(), sess: s,
	}
	r.entries[id] = e
	r.order = append(r.order, id)
	_ = r.persistEntryLocked(e)
	_ = r.persistIndexLocked()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventAdded, e.Info())
	return e, nil
}

// Kill terminates the session and removes its entry from the registry.
// The on-disk metadata directory is also removed.
func (r *Registry) Kill(id string) error {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return ErrNotFound
	}
	delete(r.entries, id)
	for i, sid := range r.order {
		if sid == id {
			r.order = append(r.order[:i], r.order[i+1:]...)
			break
		}
	}
	r.renumberLocked()
	_ = r.persistIndexLocked()
	dir := filepath.Join(SessionsDir(r.stateDir), id)
	r.mu.Unlock()

	if e.sess != nil {
		_ = e.sess.Close()
	}
	_ = os.RemoveAll(dir)
	r.broadcast(wire.SessionEventRemoved, e.Info())
	return nil
}

// Update mutates name / color / order. Pointer fields opt in.
func (r *Registry) Update(req wire.UpdateSessionReq) (*Entry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	e, ok := r.entries[req.SessionID]
	if !ok {
		return nil, ErrNotFound
	}
	if req.Name != nil {
		e.Name = *req.Name
	}
	if req.Color != nil {
		e.Color = *req.Color
	}
	if req.Order != nil {
		r.moveLocked(e.ID, *req.Order)
	}
	if err := r.persistEntryLocked(e); err != nil {
		return e, err
	}
	if err := r.persistIndexLocked(); err != nil {
		return e, err
	}
	defer r.broadcastLocked(wire.SessionEventUpdated, e.Info())
	return e, nil
}

// Subscribe returns a channel that receives every SessionEvent. The
// returned cleanup function unsubscribes and closes the channel.
// Slow consumers are dropped — listeners must drain promptly.
func (r *Registry) Subscribe() (Listener, func()) {
	ch := make(Listener, 16)
	r.mu.Lock()
	r.listeners[ch] = struct{}{}
	r.mu.Unlock()
	return ch, func() {
		r.mu.Lock()
		if _, ok := r.listeners[ch]; ok {
			delete(r.listeners, ch)
			close(ch)
		}
		r.mu.Unlock()
	}
}

// Close terminates every live session and clears listeners. The on-disk
// metadata is preserved.
func (r *Registry) Close() error {
	r.mu.Lock()
	for ch := range r.listeners {
		close(ch)
	}
	r.listeners = nil
	entries := r.entries
	r.entries = nil
	r.order = nil
	r.mu.Unlock()
	for _, e := range entries {
		if e.sess != nil {
			_ = e.sess.Close()
		}
	}
	return nil
}

// --- internal helpers below ---

func (r *Registry) moveLocked(id string, newOrder int) {
	cur := -1
	for i, s := range r.order {
		if s == id {
			cur = i
			break
		}
	}
	if cur < 0 {
		return
	}
	r.order = append(r.order[:cur], r.order[cur+1:]...)
	if newOrder < 0 {
		newOrder = 0
	}
	if newOrder > len(r.order) {
		newOrder = len(r.order)
	}
	r.order = append(r.order[:newOrder], append([]string{id}, r.order[newOrder:]...)...)
	r.renumberLocked()
}

func (r *Registry) renumberLocked() {
	for i, id := range r.order {
		if e := r.entries[id]; e != nil {
			e.Order = i
		}
	}
	// Re-persist any entries whose Order changed. We re-write all of
	// them rather than diff: the volume is small.
	for _, id := range r.order {
		if e := r.entries[id]; e != nil {
			_ = r.persistEntryLocked(e)
		}
	}
}

func (r *Registry) persistEntryLocked(e *Entry) error {
	path := filepath.Join(SessionsDir(r.stateDir), e.ID, "session.json")
	return writeJSON(path, MetaFile{
		ID: e.ID, Name: e.Name, Color: e.Color,
		Order: e.Order, Created: e.Created,
	})
}

func (r *Registry) persistIndexLocked() error {
	idx := IndexFile{Order: append([]string(nil), r.order...)}
	sort.SliceStable(idx.Order, func(i, j int) bool { return false }) // preserve order
	return writeJSON(filepath.Join(SessionsDir(r.stateDir), "index.json"), idx)
}

func (r *Registry) broadcast(kind string, info wire.SessionInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.broadcastLocked(kind, info)
}

func (r *Registry) broadcastLocked(kind string, info wire.SessionInfo) {
	ev := wire.SessionEvent{Kind: kind, Session: info}
	for ch := range r.listeners {
		select {
		case ch <- ev:
		default:
			// drop slow listener
			delete(r.listeners, ch)
			close(ch)
		}
	}
}

// pickColor returns a default color for the nth session. Six fixed
// hues that rotate; users can override via Update.
func pickColor(n int) string {
	palette := []string{
		"#f59e0b", // amber
		"#8b5cf6", // violet
		"#10b981", // emerald
		"#3b82f6", // sky
		"#ef4444", // red
		"#ec4899", // pink
	}
	return palette[n%len(palette)]
}
