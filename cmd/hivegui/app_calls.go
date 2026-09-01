// App methods that are thin control-connection RPCs: the agent
// catalog, sessions, projects, and worktrees. Each one validates its
// arguments, forwards a wire request, and lets the control read loop
// in app_control.go deliver the answer as an event. Split out of
// app.go; see app.go for the App type itself.
package main

import (
	"errors"
	"os"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

type AgentInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Color      string   `json:"color"`
	Available  bool     `json:"available"`
	InstallCmd []string `json:"installCmd,omitempty"`
}

// ListAgents returns every agent definition — built-ins plus the
// user's custom agents. The frontend uses this to populate the
// launcher menu.
func (a *App) ListAgents() []AgentInfo {
	defs := agent.All()
	out := make([]AgentInfo, 0, len(defs))
	for _, d := range defs {
		out = append(out, AgentInfo{
			ID:         string(d.ID),
			Name:       d.Name,
			Color:      d.Color,
			Available:  d.Available(),
			InstallCmd: d.InstallCmd,
		})
	}
	return out
}

// CustomAgent is the JSON shape the settings modal edits. It mirrors
// agent.Custom; camelCase tags match AgentInfo above (the snake_case
// convention applies to the daemon's wire payloads, not to these
// Wails bindings).
type CustomAgent struct {
	ID    string   `json:"id"`
	Name  string   `json:"name"`
	Cmd   []string `json:"cmd"`
	Color string   `json:"color"`
}

// ListCustomAgents returns the user's custom agent definitions as
// stored on disk, for the settings modal to edit. Invalid entries are
// included deliberately — the user has to see a broken row to fix it.
//
// A malformed agents.json is an error, not an empty list. Returning
// empty would render as "no custom agents yet" and a subsequent Save
// would overwrite the very file the user needs to repair.
func (a *App) ListCustomAgents() ([]CustomAgent, error) {
	list, err := agent.LoadCustom()
	if err != nil {
		return nil, err
	}
	out := make([]CustomAgent, 0, len(list))
	for _, c := range list {
		out = append(out, CustomAgent{ID: c.ID, Name: c.Name, Cmd: c.Cmd, Color: c.Color})
	}
	return out, nil
}

// SaveCustomAgents validates and writes the full custom-agent list,
// assigning IDs to new entries. It returns a validation error rather
// than silently dropping bad entries so the modal can show the user
// what was wrong — a warning in hived.log would be invisible to them.
//
// The daemon picks the change up on its next agent.Get; no reload
// message is needed.
func (a *App) SaveCustomAgents(list []CustomAgent) error {
	in := make([]agent.Custom, 0, len(list))
	for _, c := range list {
		in = append(in, agent.Custom{ID: c.ID, Name: c.Name, Cmd: c.Cmd, Color: c.Color})
	}
	return agent.SaveCustom(in)
}

// CreateSession asks the daemon to create a new session. agentID is
// the canonical ID from ListAgents (e.g. "claude") or "" for a
// generic shell. projectID is the owning project ("" = default).
// useWorktree, when true and the project's cwd is a git repo, makes
// the daemon spawn the session inside a fresh git worktree under
// <gitRoot>/.worktrees/. The daemon broadcasts a SESSION_EVENT(added)
// over the control connection; the frontend updates the sidebar from
// that.
// insertAfter names the session the new one should sit directly beneath
// in the display order (usually the active session); "" appends.
// branch names the worktree's branch when useWorktree is set ("" lets
// the daemon generate one). worktreePath runs the session in an
// EXISTING worktree instead of creating one — the worktree browser's
// "open a session here" action — and takes precedence over
// useWorktree.
func (a *App) CreateSession(agentID, projectID, name, color string, cols, rows int, useWorktree bool, insertAfter, branch, worktreePath string, continueConversation bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	if worktreePath != "" {
		// Resuming existing work never creates a worktree; asking for
		// both would stack a nested one inside it.
		useWorktree = false
	}
	return cs.WriteJSON(wire.FrameCreateSession, wire.CreateSpec{
		Agent:                agentID,
		ProjectID:            projectID,
		Name:                 name,
		Color:                color,
		Cols:                 cols,
		Rows:                 rows,
		UseWorktree:          useWorktree,
		Branch:               branch,
		WorktreePath:         worktreePath,
		ContinueConversation: continueConversation,
		InsertAfterSessionID: insertAfter,
	})
}

// ListWorktrees asks the daemon for the project's worktree inventory.
// The reply arrives asynchronously as the "worktree:list" event on the
// control connection — the same fanout every other control response
// uses — so the browser re-renders from the event, not from a return
// value.
func (a *App) ListWorktrees(projectID string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameListWorktrees, wire.ListWorktreesReq{ProjectID: projectID})
}

// RemoveWorktree deletes a worktree. The daemon refuses with
// "worktree_in_use", "worktree_dirty" or "worktree_unpushed" on the
// control:error channel; the GUI confirms and retries with force for
// the latter two. force never overrides the in-use refusal.
// deleteBranch additionally removes the branch the worktree was on.
func (a *App) RemoveWorktree(projectID, path string, force, deleteBranch, deleteRemote bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameRemoveWorktree, wire.RemoveWorktreeReq{
		ProjectID: projectID, Path: path, Force: force,
		DeleteBranch: deleteBranch, DeleteRemote: deleteRemote,
	})
}

// CreateWorktree materializes a worktree for a branch — normally one
// that already exists with no worktree (an orphaned branch).
func (a *App) CreateWorktree(projectID, branch string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameCreateWorktree, wire.CreateWorktreeReq{
		ProjectID: projectID, Branch: branch,
	})
}

// DeleteBranch removes a local branch that has no worktree. The daemon
// refuses with "branch_unmerged" when the branch holds commits that are
// not merged; the GUI confirms and retries with force.
func (a *App) DeleteBranch(projectID, branch string, force, deleteRemote bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameDeleteBranch, wire.DeleteBranchReq{
		ProjectID: projectID, Branch: branch, Force: force, DeleteRemote: deleteRemote,
	})
}

// RenameWorktree renames a worktree's branch and moves its directory
// to match. Refused while a session is running inside it.
func (a *App) RenameWorktree(projectID, path, newBranch string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameRenameWorktree, wire.RenameWorktreeReq{
		ProjectID: projectID, Path: path, NewBranch: newBranch,
	})
}

// DuplicateSession creates a new session pinned to an explicit cwd —
// used by the GUI's ⌘P / ⇧⌘P shortcuts to fork the active session into
// the same project + directory (and same worktree, if the source had
// one). The caller resolves the cwd on the JS side from the source
// session's worktree path or its project's cwd.
//
// UseWorktree is forced to false here: when cwd already points inside a
// worktree, we want to *reuse* it, not stack a nested worktree on top.
// Passing agentID="" creates a generic shell session.
// insertAfter is normally the id of the session being duplicated, so
// the copy lands directly beneath its source; "" appends.
func (a *App) DuplicateSession(agentID, projectID, cwd string, insertAfter string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameCreateSession, wire.CreateSpec{
		Agent:                agentID,
		ProjectID:            projectID,
		Cwd:                  cwd,
		UseWorktree:          false,
		InsertAfterSessionID: insertAfter,
	})
}

// CreateProject creates a new project.
func (a *App) CreateProject(name, color, cwd string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameCreateProject, wire.CreateProjectReq{
		Name: name, Color: color, Cwd: cwd,
	})
}

// KillProject removes a project. If killSessions is true, every
// session in the project is also killed; otherwise sessions are
// reassigned to the default project.
func (a *App) KillProject(id string, killSessions bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameKillProject, wire.KillProjectReq{
		ProjectID: id, KillSessions: killSessions,
	})
}

// UpdateProject patches name/color/cwd/order. Empty strings on
// name/color/cwd mean "no change"; -1 on order means "no change".
func (a *App) UpdateProject(id, name, color, cwd string, order int) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	req := wire.UpdateProjectReq{ProjectID: id}
	if name != "" {
		req.Name = &name
	}
	if color != "" {
		req.Color = &color
	}
	if cwd != "" {
		req.Cwd = &cwd
	}
	if order >= 0 {
		req.Order = &order
	}
	return cs.WriteJSON(wire.FrameUpdateProject, req)
}

// LaunchDir returns the cwd captured at GUI startup; useful for the
// new-project default cwd.
func (a *App) LaunchDir() string { return a.launchDir }

// PickDirectory opens the OS native folder picker and returns the
// selected path, or "" if the user cancelled. defaultDir, if
// non-empty, sets the dialog's starting location.
func (a *App) PickDirectory(defaultDir string) (string, error) {
	// macOS NSOpenPanel silently fails when DefaultDirectory points
	// at a missing path, so fall back to launchDir if the saved cwd
	// no longer exists.
	if defaultDir != "" {
		if st, err := os.Stat(defaultDir); err != nil || !st.IsDir() {
			defaultDir = ""
		}
	}
	if defaultDir == "" {
		defaultDir = a.launchDir
	}
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:                "Choose project directory",
		DefaultDirectory:     defaultDir,
		CanCreateDirectories: true,
	})
}

// Confirm shows a native yes/no dialog and reports the user's choice.
// Wails' WebKit on macOS silently no-ops window.confirm(), so the
// frontend routes confirmations through here instead.
func (a *App) Confirm(title, message string) bool {
	res, err := wruntime.MessageDialog(a.ctx, wruntime.MessageDialogOptions{
		Type:          wruntime.QuestionDialog,
		Title:         title,
		Message:       message,
		Buttons:       []string{"OK", "Cancel"},
		DefaultButton: "OK",
		CancelButton:  "Cancel",
	})
	if err != nil {
		return false
	}
	return res == "OK"
}

// OpenNewWindow spawns a second Hive GUI process. Wails v2 does not
// natively support multiple windows in a single process, so we
// re-exec the GUI binary as a detached child. The two GUIs share
// the same hived (single-instance daemon enforced by the socket
// lock), so sessions are visible from either window — each window
// can independently maximize a different session.
func (a *App) OpenNewWindow() error {
	return spawnNewGUI(a.launchDir)
}

// CloseWindow quits this GUI process. Because each window is its own
// process (multi-window is implemented by re-exec), closing the last
// window naturally ends Hive — no explicit "quit app" plumbing
// needed.
func (a *App) CloseWindow() {
	wruntime.Quit(a.ctx)
}

// KillSession asks the daemon to terminate a session. force=true
// skips the dirty-worktree safety check and discards uncommitted
// changes. Without force, killing a session whose worktree has
// uncommitted changes returns a "worktree_dirty" control error so
// the GUI can confirm with the user.
func (a *App) KillSession(id string, force bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameKillSession, wire.KillSessionReq{
		SessionID: id, Force: force,
	})
}

// KillSessionAndWorktree closes the session and deletes its worktree in
// one daemon-side operation. Kept separate from KillSession so the
// destructive variant is never reachable by passing the wrong boolean.
func (a *App) KillSessionAndWorktree(id string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameKillSession, wire.KillSessionReq{
		SessionID: id, Force: true, RemoveWorktree: true,
	})
}

// RestoreSession reopens a session closed earlier, rebuilding it from
// the tombstone the close left in the daemon's state dir. An empty id
// means "the most recently closed one" — resolved daemon-side so the
// client cannot race a retention prune between listing and restoring.
//
// The restored entry arrives on the ordinary session event stream; a
// "session:restored" event follows with whatever could not be brought
// back (scrollback never can be).
func (a *App) RestoreSession(id string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameRestoreSession, wire.RestoreSessionReq{SessionID: id})
}

// ListClosedSessions asks for the sessions that can still be reopened,
// most recently closed first. The daemon answers with a "closed:list"
// event rather than a return value, like every other control query.
func (a *App) ListClosedSessions() error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameListClosed, wire.ListClosedReq{})
}

// RestartSession asks the daemon to recycle the agent process for
// the given session in place. The session entry (name/color/order/
// worktree) is preserved; the new process uses the agent's resume
// flag (e.g. `claude --continue`) when available so the prior
// conversation is picked back up.
func (a *App) RestartSession(id string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameRestartSession, wire.RestartSessionReq{
		SessionID: id,
	})
}

// IsGitRepo reports whether path is inside a git repository. The GUI
// uses this to gate the launcher's worktree checkbox.
func (a *App) IsGitRepo(path string) bool {
	return worktree.IsGitRepo(path)
}

// OpenURL hands a URL to the OS default browser. Used by the
// xterm web-links addon when the user cmd-clicks a URL in a session.
func (a *App) OpenURL(url string) {
	if url == "" {
		return
	}
	wruntime.BrowserOpenURL(a.ctx, url)
}

// UpdateSession patches name/color/order. Empty strings on name/color
// mean "do not change"; -1 on order means "do not change".
func (a *App) UpdateSession(id, name, color string, order int) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	req := wire.UpdateSessionReq{SessionID: id}
	if name != "" {
		req.Name = &name
	}
	if color != "" {
		req.Color = &color
	}
	if order >= 0 {
		req.Order = &order
	}
	return cs.WriteJSON(wire.FrameUpdateSession, req)
}

func (a *App) requireControl() (*wire.Client, error) {
	a.mu.Lock()
	cs := a.control
	a.mu.Unlock()
	if cs == nil {
		return nil, errors.New("no control connection")
	}
	return cs, nil
}
