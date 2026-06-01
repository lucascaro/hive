// Command hive is the terminal client for hived. It lists and attaches
// to live daemon sessions from a plain terminal (e.g. over SSH), so a
// remote session is the same session the Hive GUI shows.
package main

import (
	"fmt"
	"os"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: hive <command>")
	fmt.Fprintln(os.Stderr, "commands:")
	fmt.Fprintln(os.Stderr, "  ls               list sessions")
	fmt.Fprintln(os.Stderr, "  attach [id]      attach to a session")
}

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "ls":
		fmt.Fprintln(os.Stderr, "ls: not implemented yet")
		os.Exit(1)
	case "attach":
		fmt.Fprintln(os.Stderr, "attach: not implemented yet")
		os.Exit(1)
	default:
		usage()
		os.Exit(2)
	}
}
