package registry

import (
	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
)

// TranscriptPaths resolves a session to its agent's on-disk conversation
// transcripts, oldest first. The second return is a wire.Transcript*
// reason constant, empty when the paths are usable.
//
// The three failure reasons are deliberately distinct. "Unknown session"
// and "this agent keeps no transcript" are permanent facts; "the agent
// should have one and none was found" is a transient state, and telling
// a real Claude session it has no history would be a lie the user would
// reasonably read as a bug in the feature rather than in their session.
//
// Resolution uses Entry.AgentSessionID, never Entry.ID. The two are
// equal for the common path but diverge in two real cases:
//
//   - A session created with ContinueConversation: appendSpawnArgs
//     takes the ResumeCmd branch and never injects the id flag, yet the
//     entry still records an AgentSessionID. The agent recorded its
//     conversation under an id Hive did not give it.
//   - A caller-supplied spec.Cmd: Hive does not mutate user argv, so
//     AgentSessionID stays empty.
//
// Both yield a path that does not exist, which is TranscriptMissing.
func (r *Registry) TranscriptPaths(id string) ([]string, string) {
	r.mu.Lock()
	e, ok := r.entries[id]
	if !ok {
		r.mu.Unlock()
		return nil, wire.TranscriptNoSession
	}
	agentID := e.Agent
	agentSessionID := e.AgentSessionID
	cwd := e.WorktreePath
	if cwd == "" {
		if p, ok := r.projects[e.ProjectID]; ok {
			cwd = p.Cwd
		}
	}
	r.mu.Unlock()

	def, ok := agent.Get(agent.ID(agentID))
	if !ok || def.TranscriptPaths == nil {
		// A generic shell, or an agent Hive cannot read transcripts
		// for. Not an error — just nothing to search.
		return nil, wire.TranscriptUnsupported
	}
	if agentSessionID == "" || cwd == "" {
		return nil, wire.TranscriptMissing
	}
	paths := def.TranscriptPaths(agentSessionID, cwd)
	if len(paths) == 0 {
		return nil, wire.TranscriptMissing
	}
	return paths, wire.TranscriptOK
}
