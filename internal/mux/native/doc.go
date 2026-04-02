// Package muxnative implements the native PTY multiplexer backend for Hive.
//
// This backend uses a background daemon process ("hive mux-daemon") that owns all
// PTY master file descriptors. The TUI communicates with the daemon over a Unix
// domain socket using a length-prefixed JSON protocol.
//
// # Architecture
//
//	TUI process
//	  └── backend.go (mux.Backend)
//	        └── client.go (Client)
//	              └── Unix socket (~/.config/hive/mux.sock)
//	                    └── daemon.go
//	                          └── manager.go (Pane map + PTY lifecycle)
//
// # Wire protocol (protocol.go)
//
// Each message is framed with a 4-byte big-endian length header followed by a
// JSON body. Maximum message size is 4 MiB.
//
//   - [Request]  — sent from client to daemon; Op field identifies the operation
//   - [Response] — sent from daemon to client; OK bool + result fields
//
// # Daemon lifecycle
//
// [RunDaemon] is called by cmd/mux_daemon.go. The daemon process is spawned by
// cmd/start.go via setsid so it survives TUI restarts. The socket path is
// ~/.config/hive/mux.sock (or $HIVE_CONFIG_DIR/mux.sock).
//
// # Key types
//
//   - [Manager] — owns all active [Pane] instances; handles all backend operations
//   - [Pane]    — wraps a PTY master fd, subprocess, and output ring buffer
//   - [Client]  — connects to the daemon socket and sends typed requests
package muxnative
