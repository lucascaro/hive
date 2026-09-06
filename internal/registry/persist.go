package registry

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MetaFile is what we write to <session_dir>/session.json.
type MetaFile struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	// Order is advisory. index.json's id slice is the authority and
	// is what load() derives each entry's Order from; this copy is
	// only refreshed when the entry itself is rewritten, so it goes
	// stale whenever a sibling is killed or moved.
	Order          int       `json:"order"`
	Created        time.Time `json:"created"`
	Agent          string    `json:"agent,omitempty"`           // canonical agent ID; "" = shell
	ProjectID      string    `json:"project_id,omitempty"`      // owning project; "" = default
	WorktreePath   string    `json:"worktree_path,omitempty"`   // absolute path; "" = no worktree
	WorktreeBranch string    `json:"worktree_branch,omitempty"` // branch backing the worktree
	// AgentSessionID is the agent CLI's conversation id used for
	// per-id resume across daemon restarts. For Claude this equals
	// ID (we pin it via --session-id at first launch); for Codex it's
	// the codex-generated UUID captured post-spawn from the rollout
	// file. Empty ⇔ not pinned / not yet captured / agent does not
	// support per-id resume.
	AgentSessionID string `json:"agent_session_id,omitempty"`
}

// IndexFile is what we write to sessions/index.json.
type IndexFile struct {
	Order []string `json:"order"` // session IDs in display order
}

// ProjectMetaFile is what we write to <project_dir>/project.json.
type ProjectMetaFile struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Color   string    `json:"color"`
	Cwd     string    `json:"cwd,omitempty"`
	Order   int       `json:"order"` // advisory; see MetaFile.Order
	Created time.Time `json:"created"`
}

// ProjectIndexFile is what we write to projects/index.json.
type ProjectIndexFile struct {
	Order []string `json:"order"`
}

// IdeaFile is what we write to ideas/<id>.json. There is no index
// file: display order is derived from Created (newest first), so
// nothing has to arbitrate it.
type IdeaFile struct {
	ID        string    `json:"id"`
	ProjectID string    `json:"project_id"`
	Kind      string    `json:"kind"` // wire.IdeaKind*
	Text      string    `json:"text"`
	Status    string    `json:"status"` // wire.IdeaStatus*
	Created   time.Time `json:"created"`
	Updated   time.Time `json:"updated"`
	// SourceSessionID is the session the idea was filed from, kept as
	// provenance only — the idea belongs to the project and outlives
	// that session.
	SourceSessionID string `json:"source_session_id,omitempty"`
	// SessionID is the session started from this idea.
	SessionID string `json:"session_id,omitempty"`
	// ExternalRef is reserved for the GitHub issue an idea is later
	// promoted into. Persisted but not on wire.IdeaInfo: nothing
	// renders it yet, and adding an omitempty field to the wire type
	// when something does is a zero-cost change.
	ExternalRef string `json:"external_ref,omitempty"`
}

func writeAtomic(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	return writeAtomic(path, b)
}

func readJSON(path string, v any) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}
