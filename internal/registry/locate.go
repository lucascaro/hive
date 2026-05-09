package registry

import (
	"log"
	"time"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// locatorPollInterval and locatorPollDuration control the post-Create
// polling window. The locator scans the agent's on-disk store every
// `locatorPollInterval` for up to `locatorPollDuration` waiting for
// the agent to write its first conversation file. After timeout, the
// janitor takes over for entries that are still alive.
var (
	locatorPollInterval = 500 * time.Millisecond
	locatorPollDuration = 5 * time.Second

	// janitorInterval is how often the background janitor re-scans
	// alive sessions whose ConversationID is still empty. This covers
	// the "user idle for 2h then daemon restart" case (ARCH-1).
	janitorInterval = 30 * time.Second
)

// locateConversationID polls the agent's on-disk store for up to
// locatorPollDuration trying to find a conversation ID for this
// session. On success, persists ConversationID and broadcasts an
// Updated event. On timeout, returns silently — the janitor goroutine
// will keep trying for the lifetime of the session.
func (r *Registry) locateConversationID(id string, agentID agent.ID, cwd string, created time.Time) {
	loc := agent.LocatorFor(agentID)
	if loc == nil {
		return
	}
	dataRoot := dataRootFor(agentID)
	if dataRoot == "" {
		return
	}
	deadline := time.Now().Add(locatorPollDuration)
	for {
		convID, err := loc(dataRoot, cwd, created)
		if err != nil {
			log.Printf("registry: locator %s for %s: %v", agentID, id, err)
			return
		}
		if convID != "" {
			r.setConversationID(id, convID)
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(locatorPollInterval)
	}
}

// setConversationID stores convID on the entry, persists, and
// broadcasts. No-op if the entry is gone or already has an ID set.
func (r *Registry) setConversationID(id, convID string) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok || e.ConversationID != "" {
		r.mu.Unlock()
		return
	}
	e.ConversationID = convID
	_ = r.persistEntryLocked(e)
	info := e.Info()
	r.mu.Unlock()
	r.broadcast(wire.SessionEventUpdated, info)
}

// runJanitor periodically scans alive entries with empty
// ConversationID and re-runs their locator. Stops when stopJanitor is
// signalled (Registry.Close).
func (r *Registry) runJanitor() {
	t := time.NewTicker(janitorInterval)
	defer t.Stop()
	for {
		select {
		case <-r.stopJanitor:
			return
		case <-t.C:
			r.scanForMissingConversationIDs()
		}
	}
}

// scanForMissingConversationIDs collects (id, agentID, cwd, created)
// for every alive entry with empty ConversationID, then runs each
// agent's locator out of the lock. Persists any IDs found.
func (r *Registry) scanForMissingConversationIDs() {
	type candidate struct {
		id      string
		agentID agent.ID
		cwd     string
		created time.Time
	}
	r.mu.Lock()
	candidates := make([]candidate, 0, len(r.entries))
	for id, e := range r.entries {
		if e.sess == nil || e.ConversationID != "" || e.Agent == "" {
			continue
		}
		cwd := ""
		if p, ok := r.projects[e.ProjectID]; ok {
			cwd = p.Cwd
		}
		if e.WorktreePath != "" {
			cwd = e.WorktreePath
		}
		if cwd == "" {
			continue
		}
		candidates = append(candidates, candidate{id, agent.ID(e.Agent), cwd, e.Created})
	}
	r.mu.Unlock()

	for _, c := range candidates {
		loc := agent.LocatorFor(c.agentID)
		if loc == nil {
			continue
		}
		dataRoot := dataRootFor(c.agentID)
		if dataRoot == "" {
			continue
		}
		convID, err := loc(dataRoot, c.cwd, c.created)
		if err != nil || convID == "" {
			continue
		}
		r.setConversationID(c.id, convID)
	}
}

// dataRootFor returns the agent's on-disk data root for production
// callers, or "" for agents without one. Wrapped in a var so tests
// can override (rarely needed — tests use the locator funcs directly).
var dataRootFor = func(id agent.ID) string {
	switch id {
	case agent.IDClaude:
		return agent.ClaudeProjectsRoot()
	case agent.IDCodex:
		return agent.CodexSessionsRoot()
	default:
		return ""
	}
}

