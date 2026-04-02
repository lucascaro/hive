// Package muxtmux implements the tmux multiplexer backend for Hive.
//
// This backend delegates all terminal session management to an external tmux
// binary. It implements [mux.Backend] by calling helpers from internal/tmux.
//
// Unlike the native backend, tmux sessions persist independently of the hive
// daemon — they continue running even if hive is killed. This also means
// the backend requires tmux to be installed on the host system.
//
// Attach uses "tmux attach-session"; the user presses Ctrl+B D (or the
// configured prefix+d) to detach and return to hive.
package muxtmux
