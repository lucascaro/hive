// Command hive is the terminal client for hived. It lists and attaches
// to live daemon sessions from a plain terminal (e.g. over SSH), so a
// remote session is the same session the Hive GUI shows.
package main

import (
	"fmt"
	"os"

	"github.com/lucascaro/hive/internal/client"
	"github.com/lucascaro/hive/internal/daemon"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hive <command>")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  ls               list sessions")
	fmt.Fprintln(os.Stderr, "  attach [id]      attach to a session")
}

func runLS() {
	conn, err := client.Dial(daemon.SocketPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()
	sessions, err := client.List(conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive ls:", err)
		os.Exit(1)
	}
	fmt.Print(client.FormatSessions(sessions))
}

func runAttach(args []string) {
	var idArg string
	if len(args) > 0 {
		idArg = args[0]
	}

	conn, err := client.Dial(daemon.SocketPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer conn.Close()

	// Resolve the target via a separate control connection.
	lsConn, err := client.Dial(daemon.SocketPath())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	sessions, err := client.List(lsConn)
	_ = lsConn.Close()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive attach:", err)
		os.Exit(1)
	}
	id, err := client.ResolveSession(sessions, idArg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive attach:", err)
		os.Exit(1)
	}

	t, cleanup, err := newTerm()
	if err != nil {
		fmt.Fprintln(os.Stderr, "hive attach: terminal:", err)
		os.Exit(1)
	}
	defer cleanup()

	fmt.Fprintf(os.Stderr, "attached to %s — detach with Ctrl-A d\r\n", id)
	// The daemon streams the full scrollback automatically on attach, so we
	// do NOT request a replay here — doing so would paint the history twice.
	if err := client.Attach(conn, id, t, client.AttachOptions{RequestReplay: false}); err != nil {
		cleanup()
		fmt.Fprintln(os.Stderr, "hive attach:", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		runLS()
	case "attach":
		runAttach(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}
