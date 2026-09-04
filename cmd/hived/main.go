// hived is the Hive session daemon. It owns the PTY-backed sessions
// and accepts client connections over a Unix socket. See DESIGN.md for
// the role of this binary in the architecture.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/lucascaro/hive/internal/agent"
	"github.com/lucascaro/hive/internal/daemon"
	"github.com/lucascaro/hive/internal/menubar"
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

		showVersion = flag.Bool("version", false, "print build identity and exit")
		asJSON      = flag.Bool("json", false, "with --version, print machine-readable JSON")
	)
	flag.Parse()

	if *showVersion {
		printIdentity(os.Stdout, *asJSON)
		return
	}

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

	// Point the agent catalog at the user's agents.json. The daemon
	// needs this as much as the GUI does: registry entries persist
	// only the agent ID, so every Revive/Restart re-resolves the
	// command through agent.Get.
	agent.SetCustomDir(stateDir)

	// Start the menu-bar agent, if this platform and this install have
	// one. hived owns this rather than only the GUI because the menu
	// bar reports on the daemon: it should exist exactly as long as
	// there is a daemon to report on, including when every window is
	// closed. Best-effort and non-blocking; hivebar's own lock sorts
	// out the race with hivegui doing the same thing.
	menubar.Spawn()

	if f, err := registry.OpenLogFile("hived.log"); err == nil {
		log.SetOutput(io.MultiWriter(os.Stderr, f))
		log.Printf("hived: log tee to %s", filepath.Join(stateDir, "hived.log"))
	}

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

	// Write a pidfile NEXT TO the socket so the GUI's Restart action
	// can scope its SIGTERM to the daemon owning the socket it just
	// dialed. (Earlier the pidfile lived at $STATE/hived.pid — global,
	// which broke if the user had a second hived running with a custom
	// --socket: the GUI could end up signaling the wrong instance.)
	// Done AFTER daemon.New so a second hived that loses the bind
	// race doesn't clobber the running daemon's pidfile and then leave
	// it stale (log.Fatalf below skips defers).
	pidPath := d.SocketPath() + ".pid"
	if err := os.WriteFile(pidPath, fmt.Appendf(nil, "%d", os.Getpid()), 0o600); err != nil {
		log.Printf("hived: write pidfile: %v", err)
	}
	defer removePidfile(pidPath)

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

// removePidfile deletes the pidfile, but only when it still names
// this process. A daemon that exits slowly (or is killed after its
// replacement is already up) would otherwise delete the *live*
// daemon's pidfile on the way out, leaving the GUI's Restart action
// with no handle on the running daemon at all.
//
// Logs any failure other than "already gone" — a stale pidfile makes
// the GUI's Restart action signal the wrong process, so a failed
// cleanup must be diagnosable from hived.log.
func removePidfile(path string) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("hived: read pidfile %s before removal: %v", path, err)
		}
		return
	}
	if owner := strings.TrimSpace(string(raw)); owner != strconv.Itoa(os.Getpid()) {
		log.Printf("hived: pidfile %s now owned by pid %s, leaving it alone", path, owner)
		return
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		log.Printf("hived: remove pidfile %s: %v", path, err)
	}
}
