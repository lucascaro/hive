// hivec-spike is the throwaway client for Phase 0 Spike A.
//
// It connects to hived-spike over a Unix socket, puts the local terminal
// in raw mode, pipes stdin to the daemon and the daemon's output to
// stdout, and forwards SIGWINCH as resize frames.
//
// Detach: single Ctrl-Q (0x11). The daemon and its shell keep running.
// Run hivec-spike again to reattach.
//
// PTY→stdout output is filtered through proto.KittyFilter to strip
// kitty-keyboard-protocol enable/disable CSI sequences. This keeps the
// local outer terminal in legacy keyboard mode regardless of what the
// remote shell or its child programs (claude, vim) try to enable.
// Without this filter, Ctrl-Q arrives as a multi-byte CSI escape and
// the single-byte detach check never fires — exactly what the user
// reported when running claude or modern vim through the spike.
//
// On send-key disable: send Ctrl-Q to the remote by typing Ctrl-Q twice
// (the second byte is forwarded as a literal).
package main

import (
	"log"
	"net"
	"os"

	"github.com/lucascaro/hive/spikes/spike-a-daemon/internal/proto"
	"golang.org/x/term"
)

const detachKey byte = 0x11 // Ctrl-Q

func sendResize(c net.Conn) {
	cols, rows, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil {
		return
	}
	_ = proto.WriteFrame(c, proto.FrameResize, proto.EncodeResize(uint16(cols), uint16(rows)))
}

func main() {
	conn, err := net.Dial("unix", proto.SocketPath(os.Getuid()))
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		log.Fatalf("stdin is not a terminal")
	}
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		log.Fatalf("make raw: %v", err)
	}
	defer func() { _ = term.Restore(fd, oldState) }()

	sendResize(conn)
	winch := watchResize(conn)

	done := make(chan struct{})
	// stdin → daemon. Sniff for Ctrl-Q as the detach trigger.
	// To send a literal Ctrl-Q to the remote, type Ctrl-Q twice: the
	// first is consumed (treated as detach-pending), the second is
	// forwarded.
	go func() {
		defer close(done)
		buf := make([]byte, 4096)
		detachPending := false
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				out := make([]byte, 0, n)
				detach := false
				for _, b := range buf[:n] {
					if detachPending {
						detachPending = false
						if b == detachKey {
							// Ctrl-Q Ctrl-Q → forward one literal Ctrl-Q.
							out = append(out, detachKey)
							continue
						}
						// Ctrl-Q + something else → that "something else"
						// is the detach trigger? No: Ctrl-Q alone detaches.
						// Pending is only carried within a single read so
						// that Ctrl-Q Ctrl-Q sends a literal; otherwise we
						// detached before getting here.
						detach = true
						break
					}
					if b == detachKey {
						// First Ctrl-Q in this chunk: defer decision in case
						// the very next byte is also Ctrl-Q (literal escape).
						detachPending = true
						continue
					}
					out = append(out, b)
				}
				if len(out) > 0 {
					if err := proto.WriteFrame(conn, proto.FrameData, out); err != nil {
						return
					}
				}
				if detach || (detachPending && err != nil) {
					_ = conn.Close()
					return
				}
				// If detachPending is set and we have no more bytes in
				// this chunk, peek the NEXT read. To keep things simple
				// for the spike: a lone trailing Ctrl-Q at end-of-chunk
				// will detach on the next read or on EOF.
				if detachPending {
					// Wait for next byte to decide. Read one more byte.
					var one [1]byte
					m, rerr := os.Stdin.Read(one[:])
					if m == 1 {
						detachPending = false
						if one[0] == detachKey {
							// literal Ctrl-Q
							_ = proto.WriteFrame(conn, proto.FrameData, []byte{detachKey})
							continue
						}
						// Anything else after a Ctrl-Q means detach now,
						// then forward whatever the user pressed... but
						// since we've already detached, just exit.
						_ = conn.Close()
						return
					}
					if rerr != nil {
						_ = conn.Close()
						return
					}
				}
			}
			if err != nil {
				return
			}
		}
	}()

	// daemon → stdout, with kitty-keyboard CSI sequences stripped.
	go func() {
		filter := &proto.KittyFilter{}
		for {
			t, data, err := proto.ReadFrame(conn)
			if err != nil {
				stopResize(winch)
				return
			}
			if t == proto.FrameData {
				_, _ = os.Stdout.Write(filter.Filter(data))
			}
		}
	}()

	<-done
}
