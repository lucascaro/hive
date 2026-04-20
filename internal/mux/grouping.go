package mux

// GroupedBackend is an optional extension implemented by backends that
// support per-instance grouped sessions. tmux implements it; the native PTY
// backend does not (multi-instance independence is tmux-only).
//
// The interface is exposed via type assertion rather than added to Backend so
// backends without this concept stay free of boilerplate no-op methods.
type GroupedBackend interface {
	InitInstance() error
	ShutdownInstance() error
	SweepOrphanInstances() error
	InstanceSession() string
	CanonicalExists() bool
}

// InitInstance creates this process's grouped session. No-op on backends that
// do not implement GroupedBackend (the native backend).
func InitInstance() error {
	if gb, ok := active.(GroupedBackend); ok {
		return gb.InitInstance()
	}
	return nil
}

// ShutdownInstance tears down this process's grouped session. No-op on
// backends that do not implement GroupedBackend.
func ShutdownInstance() error {
	if gb, ok := active.(GroupedBackend); ok {
		return gb.ShutdownInstance()
	}
	return nil
}

// SweepOrphanInstances reclaims grouped sessions left behind by crashed hive
// processes. No-op on backends that do not implement GroupedBackend.
func SweepOrphanInstances() error {
	if gb, ok := active.(GroupedBackend); ok {
		return gb.SweepOrphanInstances()
	}
	return nil
}

// InstanceSession returns the grouped-session name attached to this process.
// Returns the canonical HiveSession name when the backend does not support
// grouping or no instance has been initialised — preserving the single-
// session semantics callers assumed before multi-instance support landed.
func InstanceSession() string {
	if gb, ok := active.(GroupedBackend); ok {
		if name := gb.InstanceSession(); name != "" {
			return name
		}
	}
	return HiveSession
}

// CanonicalExists reports whether the backend's canonical session is alive.
// Returns true on backends that do not implement GroupedBackend (they have
// no separate canonical concept, so the question is meaningless and we
// default to "fine").
func CanonicalExists() bool {
	if gb, ok := active.(GroupedBackend); ok {
		return gb.CanonicalExists()
	}
	return true
}
