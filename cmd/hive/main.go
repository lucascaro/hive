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

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		runLS()
	case "attach":
		fmt.Fprintln(os.Stderr, "attach: not implemented yet")
		os.Exit(1)
	default:
		usage()
		os.Exit(2)
	}
}
