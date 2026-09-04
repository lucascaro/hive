//go:build darwin

package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	hdaemon "github.com/lucascaro/hive/internal/daemon"
)

// enclosingAppBundle walks up from p to the nearest .app directory,
// returning "" when there is none (a bare binary in a dev tree).
func enclosingAppBundle(p string) string {
	for dir := filepath.Dir(p); dir != "/" && dir != "."; dir = filepath.Dir(dir) {
		if strings.HasSuffix(dir, ".app") {
			return dir
		}
	}
	return ""
}

// hiveAppBundle resolves the Hive.app this hivebar ships inside.
//
// hivebar lives at <Hive>.app/Contents/Library/LoginItems/hivebar.app,
// so the OUTER bundle is four levels above hivebar's own. Walking to
// the nearest .app finds hivebar's own bundle; this keeps walking.
func hiveAppBundle() string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	own := enclosingAppBundle(self)
	if own == "" {
		return ""
	}
	// Contents/Library/LoginItems/<own> -> the bundle containing it.
	return enclosingAppBundle(filepath.Dir(own))
}

// locateHived finds the daemon binary, mirroring cmd/hivegui/locate.go
// so a dev tree and an installed bundle both work.
func locateHived() (string, error) {
	if p := os.Getenv("HIVED"); p != "" && isExecutable(p) {
		return p, nil
	}
	if app := hiveAppBundle(); app != "" {
		if c := filepath.Join(app, "Contents", "MacOS", "hived"); isExecutable(c) {
			return c, nil
		}
	}
	if self, err := os.Executable(); err == nil {
		if c := filepath.Join(filepath.Dir(self), "hived"); isExecutable(c) {
			return c, nil
		}
	}
	if p, err := exec.LookPath("hived"); err == nil {
		return p, nil
	}
	return "", fmt.Errorf("hived binary not found")
}

func isExecutable(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// OpenGUI launches (or brings forward) the Hive window.
//
// Without -n, so an already-running GUI is activated rather than
// duplicated: the menu bar's "Open Hive" means "show me the window",
// and spawning a second one every click would be a slow way to end up
// with nine of them.
func OpenGUI() error {
	app := hiveAppBundle()
	if app == "" {
		return fmt.Errorf("not running from a Hive.app bundle")
	}
	return exec.Command("open", app).Run()
}

// spawnHived starts a detached daemon, the same way the GUI does.
func spawnHived() error {
	bin, err := locateHived()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
	return cmd.Start()
}

// RestartDaemon stops hived and starts a fresh one.
//
// The shutdown is sent in-band by the caller (the control connection is
// the only handle hivebar has); this waits for the socket to actually
// go quiet before spawning, because a new daemon that loses the bind
// race to the old one exits immediately and leaves the user with the
// daemon they asked to replace.
//
// Unlike the GUI's RestartDaemon this does NOT relaunch any window.
// hivebar has none, and the GUI's own reconnect loop picks the new
// daemon up.
func RestartDaemon(shutdown func()) error {
	sock := hdaemon.SocketPath()
	shutdown()
	if !socketDead(sock, 5*time.Second) {
		return fmt.Errorf("hived still answering on %s; not restarting", sock)
	}
	if err := spawnHived(); err != nil {
		return fmt.Errorf("start hived: %w", err)
	}
	log.Printf("hivebar: restarted hived")
	return nil
}

// socketDead reports whether nothing answers on sock within budget.
// Dialling, not signalling: a zombie daemon answers signal(0) forever
// while holding no socket, and what matters is whether a new daemon can
// take the address. Same reasoning as cmd/hivegui/restart.go.
func socketDead(sock string, budget time.Duration) bool {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		c, err := net.DialTimeout("unix", sock, 500*time.Millisecond)
		if err != nil {
			return true
		}
		_ = c.Close()
		time.Sleep(200 * time.Millisecond)
	}
	return false
}

// Confirm shows a native modal and reports whether the user agreed.
//
// osascript rather than a Go dialog library: hivebar is an accessory
// app with no window of its own, and pulling in a UI toolkit to ask one
// yes/no question would be a large dependency for one string. Returns
// false on any error, so a broken dialog can never be read as consent
// for something that ends every running agent.
func Confirm(title, body string) bool {
	// Through System Events, and with an explicit `activate`, because
	// hivebar is an LSUIElement accessory: a dialog it puts up on its
	// own can land behind whatever the user is looking at. A confirm
	// nobody sees in front of "this ends every running agent" is worse
	// than no confirm at all — it makes Restart Daemon look broken.
	//
	// `default button "Cancel"` so Return does the safe thing.
	script := fmt.Sprintf(
		`tell application "System Events"
			activate
			display dialog %s with title %s buttons {"Cancel", "Continue"} default button "Cancel" with icon caution
		end tell`,
		asAppleScriptString(body), asAppleScriptString(title))
	// -e, and the strings quoted above, so nothing here is shell- or
	// AppleScript-injectable. The text is ours, but session names reach
	// some of these dialogs and those come from the user's shell.
	out, err := exec.Command("osascript", "-e", script).Output()
	if err != nil {
		// A cancelled dialog exits non-zero, which is the same as an
		// error here — both mean "did not agree".
		return false
	}
	return strings.Contains(string(out), "Continue")
}

// asAppleScriptString quotes s as an AppleScript literal.
func asAppleScriptString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"', '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
