// App methods that are thin control-connection RPCs: the agent
// catalog, sessions, projects, and worktrees. Each one validates its
// arguments, forwards a wire request, and lets the control read loop
// in app_control.go deliver the answer as an event. Split out of
// app.go; see app.go for the App type itself.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
	"github.com/lucascaro/hive/internal/worktree"
)

type AgentInfo struct {
	ID         string   `json:"id"`
	Name       string   `json:"name"`
	Color      string   `json:"color"`
	Available  bool     `json:"available"`
	InstallCmd []string `json:"installCmd,omitempty"`
	// TakesPrompt reports whether this agent can be handed an opening
	// prompt at all — either as argv or typed into its prompt box. The
	// launcher needs it because it offers the prompt box BEFORE the
	// agent is chosen: without it, picking the shell agent (the first
	// row) silently discards what the user wrote. The daemon is still
	// the authority; this only stops the GUI promising something it
	// will refuse.
	TakesPrompt bool `json:"takesPrompt"`
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
			// Mirrors registry.deliveryFor's two positive cases. A
			// custom agent is neither: validateCustom builds its Def
			// with ID/Name/Cmd/Color only, so both flags are false by
			// construction.
			TakesPrompt: d.PositionalPrompt || d.TypedPrompt,
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

// CreateSessionOpts is the request CreateSession takes. A struct, not
// a parameter list: the positional form had reached twelve arguments,
// which is well past the point where the next reader can call it
// correctly, and Wails names each field in the generated TS binding.
//
// Zero values mean "unset" throughout, so the frontend passes only the
// fields an opening actually decides.
type CreateSessionOpts struct {
	// Agent is the canonical ID from ListAgents (e.g. "claude"), or ""
	// for a generic shell.
	Agent string `json:"agent"`
	// Project is the owning project; "" means the default project.
	Project string `json:"project"`
	Name    string `json:"name"`
	Color   string `json:"color"`
	Cols    int    `json:"cols"`
	Rows    int    `json:"rows"`
	// UseWorktree, when true and the project's cwd is a git repo, makes
	// the daemon spawn the session inside a fresh git worktree under
	// <gitRoot>/.worktrees/.
	UseWorktree bool `json:"useWorktree"`
	// InsertAfter names the session the new one should sit directly
	// beneath in the display order (usually the active session); ""
	// appends.
	InsertAfter string `json:"insertAfter"`
	// Branch names the worktree's branch when UseWorktree is set; ""
	// lets the daemon generate one.
	Branch string `json:"branch"`
	// WorktreePath runs the session in an EXISTING worktree instead of
	// creating one — the worktree browser's "open a session here"
	// action — and takes precedence over UseWorktree.
	WorktreePath string `json:"worktreePath"`
	// ContinueConversation asks the agent to pick up its most recent
	// conversation in that worktree rather than starting a fresh one.
	ContinueConversation bool `json:"continueConversation"`
	// InitialPrompt is the opening turn to seed the agent with — the
	// inbox's "Start session" action. Claude and Pi take it as an argv
	// positional; every other agent has it typed into the PTY once the
	// session first goes idle.
	InitialPrompt string `json:"initialPrompt"`
	// IdeaID is the idea the session is being started from. The daemon
	// flips it to `started` once the prompt is delivered.
	IdeaID string `json:"ideaId"`
}

// CreateSession asks the daemon to create a new session. The daemon
// broadcasts a SESSION_EVENT(added) over the control connection; the
// frontend updates the sidebar from that, so there is no session id to
// return here.
func (a *App) CreateSession(opts CreateSessionOpts) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	if opts.WorktreePath != "" {
		// Resuming existing work never creates a worktree; asking for
		// both would stack a nested one inside it.
		opts.UseWorktree = false
	}
	return cs.WriteJSON(wire.FrameCreateSession, wire.CreateSpec{
		Agent:                opts.Agent,
		ProjectID:            opts.Project,
		Name:                 opts.Name,
		Color:                opts.Color,
		Cols:                 opts.Cols,
		Rows:                 opts.Rows,
		UseWorktree:          opts.UseWorktree,
		Branch:               opts.Branch,
		WorktreePath:         opts.WorktreePath,
		ContinueConversation: opts.ContinueConversation,
		InsertAfterSessionID: opts.InsertAfter,
		InitialPrompt:        opts.InitialPrompt,
		IdeaID:               opts.IdeaID,
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

// SetWorktreeLabel names a worktree group, or clears the name when
// label is empty. It renames nothing — not the sessions in the
// worktree, not the branch, not the directory — and unlike the other
// worktree calls it is allowed while sessions are running inside it.
func (a *App) SetWorktreeLabel(projectID, path, label string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameSetWorktreeLabel, wire.SetWorktreeLabelReq{
		ProjectID: projectID, Path: path, Label: label,
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
// reassigned to the default project. deleteIdeas is the confirmation
// override for project_has_ideas: the daemon refuses while the project
// still holds open ideas, and the GUI retries with it set once the user
// has said yes.
func (a *App) KillProject(id string, killSessions, deleteIdeas bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameKillProject, wire.KillProjectReq{
		ProjectID: id, KillSessions: killSessions, DeleteIdeas: deleteIdeas,
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

// StateDirID identifies the daemon registry this GUI is attached to,
// as the first 8 hex chars of sha256(registry.StateDir()).
//
// Every hivegui process shares ONE webview localStorage — WKWebView
// keys its store on the bundle id, not on the socket — while each
// daemon owns a registry with its own project UUIDs. The frontend
// suffixes its persisted project-id sets with this value so a GUI on
// one state dir stops pruning away another's ids as "projects that no
// longer exist" (#340).
//
// Hashed rather than returned raw so a filesystem path never lands in
// web storage; truncated because 8 hex chars is plenty to separate the
// handful of state dirs one machine ever has.
func (a *App) StateDirID() string {
	// Cleaned first: the same directory spelled two ways (trailing
	// slash, a relative path) would otherwise hash to different buckets
	// and silently start that instance from a clean slate.
	sum := sha256.Sum256([]byte(filepath.Clean(registry.StateDir())))
	return hex.EncodeToString(sum[:])[:8]
}

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
	return confirmAccepted(res)
}

// confirmAccepted reports whether a MessageDialog result is the
// affirmative answer. The label we get back is the backend's choice,
// not ours: macOS honours the Buttons slice above and returns "OK", but
// Wails' Windows backend ignores Buttons entirely — a QuestionDialog
// becomes MB_YESNO, so the user sees native Yes/No and the Win32 code
// is mapped through a fixed table whose affirmatives are "Yes" (IDYES)
// and "Ok" (IDOK, lowercase k — "OK" never appears). Matching only
// "OK" made Confirm return false forever on Windows, silently
// no-opping every confirm-gated action. See TestConfirmAccepted.
func confirmAccepted(res string) bool {
	switch res {
	case "OK", "Ok", "Yes":
		return true
	}
	return false
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

// OpenURL hands a URL to the OS default browser. Used by the xterm
// web-links addon and the OSC 8 link handler when the user clicks a
// URL in a session.
//
// Only web and mail schemes are forwarded. Terminal content is
// attacker-influenced (agent output, a cat'd README), and OSC 8 lets
// it label a file:// or custom-scheme URI with any visible text;
// BrowserOpenURL would launch whatever handles that scheme.
func (a *App) OpenURL(rawURL string) {
	if !allowedURL(rawURL) {
		log.Printf("hivegui: refusing to open URL with disallowed scheme: %q", rawURL)
		return
	}
	wruntime.BrowserOpenURL(a.ctx, rawURL)
}

// allowedURL reports whether raw parses as an absolute URL whose
// scheme is http, https or mailto.
func allowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		return u.Host != ""
	case "mailto":
		return u.Opaque != ""
	}
	return false
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

// SetSessionAttention tells the daemon whether a session still wants
// the user's attention. The GUI calls it with false when the user
// focuses a session — the daemon sets the flag from the terminal bell,
// but only a client knows the user has actually looked.
//
// Its own binding rather than another positional argument on
// UpdateSession: everything that one carries is persisted state
// broadcast as "updated", and this is neither. The daemon routes the
// two apart for that reason, and a shared entry point would invite
// them back together.
func (a *App) SetSessionAttention(id string, want bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameUpdateSession, wire.UpdateSessionReq{
		SessionID:      id,
		NeedsAttention: &want,
	})
}

// ---------- ideas ----------
//
// Same shape as the project and session calls above: a request goes
// out, and the daemon's answer (IDEAS, or an IDEA_EVENT fanned out to
// every control connection) arrives through the read loop in
// app_control.go as an "idea:list" / "idea:event" event.

// ListIdeas asks for one project's ideas, or for every project's when
// projectID is empty. The frontend calls it once at boot; after that
// the fan-out keeps it current.
func (a *App) ListIdeas(projectID string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameListIdeas, wire.ListIdeasReq{ProjectID: projectID})
}

// AddIdea files one idea. sessionID is the session it was captured
// from and may be empty; projectID may be empty too, in which case the
// daemon resolves it from sessionID's live registry entry. An empty
// kind means "idea".
func (a *App) AddIdea(sessionID, projectID, kind, text string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameAddIdea, wire.AddIdeaReq{
		SessionID: sessionID, ProjectID: projectID, Kind: kind, Text: text,
	})
}

// UpdateIdea patches an idea. Empty strings mean "do not change", the
// same convention as UpdateProject/UpdateSession — none of these
// fields has a legitimate empty value: the text is never empty, and
// status, kind and project id are closed sets with no empty member.
//
// kind and projectID are the inbox's correction path: the capture
// sheet pre-fills the project from whatever session was focused, so a
// mis-filed note is an ordinary mistake rather than user error.
func (a *App) UpdateIdea(id, text, status, sessionID, kind, projectID string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	req := wire.UpdateIdeaReq{ID: id}
	if text != "" {
		req.Text = &text
	}
	if status != "" {
		req.Status = &status
	}
	if sessionID != "" {
		req.SessionID = &sessionID
	}
	if kind != "" {
		req.Kind = &kind
	}
	if projectID != "" {
		req.ProjectID = &projectID
	}
	return cs.WriteJSON(wire.FrameUpdateIdea, req)
}

// ResolvePrompt settles a session's pending opening prompt: paste
// places it in the agent's input box (unsubmitted — the user presses
// Enter), dismiss discards it. Either way the affordance goes away.
//
// The daemon does the writing. The GUI never opens a PTY (DESIGN.md),
// and this keeps one code path for what text a session's agent
// receives.
func (a *App) ResolvePrompt(sessionID string, paste bool) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameResolvePrompt, wire.ResolvePromptReq{
		SessionID: sessionID, Paste: paste,
	})
}

// RemoveIdea deletes one idea outright. The GUI confirms first.
func (a *App) RemoveIdea(id string) error {
	cs, err := a.requireControl()
	if err != nil {
		return err
	}
	return cs.WriteJSON(wire.FrameRemoveIdea, wire.RemoveIdeaReq{ID: id})
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
