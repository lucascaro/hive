// Package client implements the hive terminal CLI: it speaks the
// internal/wire protocol to a running hived over its Unix socket,
// listing and attaching to sessions from a plain terminal.
package client

import (
	"fmt"
	"net"
)

// ClientName is the wire Hello.Client identifier for this binary.
func ClientName() string { return "hive-cli/0.1" }

// Dial connects to a running hived at socketPath. Unlike the GUI, it
// never spawns a daemon — if nothing is listening it returns a friendly
// error pointing at the most likely cause.
func Dial(socketPath string) (net.Conn, error) {
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("cannot reach hived at %s — is Hive running? (%w)", socketPath, err)
	}
	return conn, nil
}
