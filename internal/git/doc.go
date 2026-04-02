// Package git provides helpers for managing Git worktrees for Hive sessions.
//
// When a session is created with the "worktree" option, Hive creates an isolated
// Git working tree (via git worktree add) so the agent can work on a dedicated
// branch without affecting other sessions in the same repository.
//
// # Key functions
//
//   - [IsGitRepo]      — reports whether a directory is inside a Git repository
//   - [CreateWorktree] — creates a new worktree on a new branch in a temp directory
//   - [RemoveWorktree] — removes the worktree and prunes the reference
//
// Worktree paths and branch names are stored on [state.Session] so they can be
// cleaned up when the session is killed.
package git
