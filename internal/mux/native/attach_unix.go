//go:build !windows

package muxnative

import (
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"
)

// detachKey is Ctrl+Q (0x11). Pressing it while attached returns to the TUI
// without terminating the running process.
const detachKey = 0x11

// clientAttach connects to the daemon, requests an attach for target, then
// proxies stdin/stdout between the current terminal and the daemon.
//
// Protocol after the initial JSON handshake:
//   - client → daemon: raw stdin bytes (Ctrl+Q is intercepted and not forwarded)
//   - daemon → client: raw PTY output bytes
//
// The client signals detach by half-closing its write side of the socket.
// The daemon drains any remaining PTY output and then closes the connection.
func clientAttach(c *daemonClient, target string) error {
	conn, err := c.dialRaw()
	if err != nil {
		return err
	}
	defer conn.Close()

	// Send attach request and wait for OK.
	if err := writeMsg(conn, Request{Op: "attach", Target: target}); err != nil {
		return err
	}
	var resp Response
	if err := readMsg(conn, &resp); err != nil {
		return err
	}
	if !resp.OK {
		return fmt.Errorf("attach: %s", resp.Error)
	}

	// Put stdin in raw mode so all keystrokes go directly to the proxy.
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		return fmt.Errorf("set raw mode: %w", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState) //nolint:errcheck

	// Handle terminal resize: send SIGWINCH to ourselves (no-op for now; the
	// daemon's PTY was sized at process start). Suppressing the signal prevents
	// the default shell behaviour of printing "window changed".
	winch := make(chan os.Signal, 1)
	signal.Notify(winch, syscall.SIGWINCH)
	defer signal.Stop(winch)
	go func() {
		for range winch {
			// Resize support can be added here in the future.
		}
	}()

	done := make(chan struct{})

	// daemon → stdout
	go func() {
		defer close(done)
		io.Copy(os.Stdout, conn) //nolint:errcheck
	}()

	// stdin → daemon (Ctrl+Q intercept)
	go func() {
		buf := make([]byte, 256)
		uc := conn.(interface{ CloseWrite() error })
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil || n == 0 {
				uc.CloseWrite() //nolint:errcheck
				return
			}
			for i := 0; i < n; i++ {
				if buf[i] == detachKey {
					uc.CloseWrite() //nolint:errcheck
					return
				}
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				uc.CloseWrite() //nolint:errcheck
				return
			}
		}
	}()

	<-done
	return nil
}
