// Package hooks discovers and executes shell hook scripts for Hive lifecycle events.
//
// Hook scripts are executable files in ~/.config/hive/hooks/ (or
// config.HooksConfig.Dir). Two naming patterns are supported:
//
//   - Flat file:   on-{event}         (e.g., on-session-create)
//   - Directory:   on-{event}.d/      (scripts run in alphabetical order)
//
// # Usage
//
//	errs := hooks.Run(cfg.Hooks.Dir, state.HookEvent{
//	    Name:        state.EventSessionCreate,
//	    ProjectID:   project.ID,
//	    ProjectName: project.Name,
//	    SessionID:   sess.ID,
//	    // ...
//	})
//
// Errors are returned but are non-fatal; callers should log them. Each script
// has a 5-second timeout and is killed if it exceeds that limit.
//
// # Environment
//
// [BuildEnv] constructs the HIVE_* environment variables injected into each
// hook script. See docs/hooks.md for the full variable list.
package hooks
