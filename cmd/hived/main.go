// hived is the Hive session daemon. It owns one PTY-backed shell
// session and accepts client connections over a Unix socket. See
// docs/native-rewrite/phase-1.md for the role of this binary in the
// native rewrite.
package main

import (
	"context"
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/session"
)

func main() {
	var (
		sock  = flag.String("socket", "", "Unix socket path (empty = platform default)")
		shell = flag.String("shell", "", "shell to run (empty = $SHELL or platform default)")
		cols  = flag.Int("cols", 80, "initial PTY width in columns")
		rows  = flag.Int("rows", 24, "initial PTY height in rows")
	)
	flag.Parse()

	d, err := daemon.New(daemon.Config{
		SocketPath: *sock,
		Session: session.Options{
			Shell: *shell,
			Cols:  *cols,
			Rows:  *rows,
		},
	})
	if err != nil {
		log.Fatalf("hived: %v", err)
	}
	defer d.Close()

	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("hived: shutting down")
		cancel()
	}()

	if err := d.Run(ctx); err != nil {
		log.Fatalf("hived: run: %v", err)
	}
}
