// Package client implements the hive terminal CLI: it speaks the
// internal/wire protocol to a running hived over its Unix socket,
// listing and attaching to sessions from a plain terminal.
package client

// ClientName is the wire Hello.Client identifier for this binary.
func ClientName() string { return "hive-cli/0.1" }
