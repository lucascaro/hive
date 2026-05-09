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
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Color          string    `json:"color"`
	Order          int       `json:"order"`
	Created        time.Time `json:"created"`
	Agent          string    `json:"agent,omitempty"`           // canonical agent ID; "" = shell
	ProjectID      string    `json:"project_id,omitempty"`      // owning project; "" = default
	WorktreePath   string    `json:"worktree_path,omitempty"`   // absolute path; "" = no worktree
	WorktreeBranch string    `json:"worktree_branch,omitempty"` // branch backing the worktree
	ConversationID string    `json:"conversation_id,omitempty"` // agent-specific conversation ID for ID-based resume
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
	Order   int       `json:"order"`
	Created time.Time `json:"created"`
}

// ProjectIndexFile is what we write to projects/index.json.
type ProjectIndexFile struct {
	Order []string `json:"order"`
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
