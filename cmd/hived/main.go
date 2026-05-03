// hived is the Hive session daemon. It owns one PTY-backed shell
// session and accepts client connections over a Unix socket. See
// docs/native-rewrite/phase-1.md for the role of this binary in the
// native rewrite.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/session"
)

func main() {
	var (
		sock  = flag.String("socket", "", "Unix socket path (empty = platform default)")
		shell = flag.String("shell", "", "shell to run (empty = $SHELL or platform default)")
		cwd   = flag.String("cwd", "", "default working directory for new sessions")
		cols  = flag.Int("cols", 80, "initial PTY width in columns")
		rows  = flag.Int("rows", 24, "initial PTY height in rows")
	)
	flag.Parse()

	// Chdir to the user-supplied launch directory so session.Start's
	// os.Getwd() fallback picks it up for any session created without
	// an explicit Cwd.
	if *cwd != "" {
		if err := os.Chdir(*cwd); err != nil {
			log.Printf("hived: chdir %s: %v", *cwd, err)
		}
	}

	// Tee logs to a file under the state dir so the GUI's auto-spawned
	// daemon (whose stdout/stderr are /dev/null) leaves a paper trail.
	stateDir := registry.StateDir()
	if err := os.MkdirAll(stateDir, 0o700); err == nil {
		logPath := filepath.Join(stateDir, "hived.log")
		if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600); err == nil {
			log.SetOutput(io.MultiWriter(os.Stderr, f))
			log.Printf("hived: log tee to %s", logPath)
		}
	}

	// Write a pidfile so hivegui's "Restart daemon" action can find
	// and signal us. Best-effort; cleared on clean shutdown.
	pidPath := filepath.Join(stateDir, "hived.pid")
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", os.Getpid())), 0o600); err != nil {
		log.Printf("hived: write pidfile: %v", err)
	}
	defer os.Remove(pidPath)

	d, err := daemon.New(daemon.Config{
		SocketPath: *sock,
		BootstrapSession: session.Options{
			Shell: *shell,
			Cwd:   *cwd,
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
