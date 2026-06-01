package client

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"

	"github.com/lucascaro/hive/internal/wire"
)

// Terminal abstracts the local terminal so Attach is testable without a
// real TTY. Stdin/Stdout are the raw byte streams; Size reports the
// current dimensions; ResizeEvents fires whenever the size changes.
type Terminal interface {
	Stdin() io.Reader
	Stdout() io.Writer
	Size() (cols, rows int, err error)
	ResizeEvents() <-chan struct{}
}

// AttachOptions tunes attach behavior.
type AttachOptions struct {
	// RequestReplay asks the daemon to repaint scrollback on attach.
	RequestReplay bool
}

// Attach performs the attach handshake on conn (freshly dialed) for
// sessionID, then streams PTY data to term.Stdout and term keystrokes
// to the session until the user detaches (Ctrl-A d) or the connection
// closes. It returns nil on a clean detach or session exit.
//
// After Attach returns the caller must close conn; an internal reader
// goroutine blocks on conn until the connection is closed.
func Attach(conn net.Conn, sessionID string, term Terminal, opts AttachOptions) error {
	if err := wire.WriteJSON(conn, wire.FrameHello, wire.Hello{
		Version:   wire.PROTOCOL_VERSION,
		Client:    ClientName(),
		Mode:      wire.ModeAttach,
		SessionID: sessionID,
	}); err != nil {
		return fmt.Errorf("attach hello: %w", err)
	}
	ft, payload, err := wire.ReadFrame(conn)
	if err != nil {
		return fmt.Errorf("attach welcome: %w", err)
	}
	if ft == wire.FrameError {
		var werr wire.Error
		if jsonUnmarshal(payload, &werr) == nil && werr.Message != "" {
			return fmt.Errorf("attach rejected: %s", werr.Message)
		}
		return errors.New("attach rejected by daemon")
	}
	if ft != wire.FrameWelcome {
		return fmt.Errorf("attach: expected WELCOME, got %s", ft)
	}

	// Serialize all post-handshake writes: wire.WriteFrame issues two
	// separate conn.Write calls (header then payload), so concurrent
	// writers (writer loop + resize goroutine) would interleave frames.
	var writeMu sync.Mutex
	writeFrame := func(t wire.FrameType, p []byte) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return wire.WriteFrame(conn, t, p)
	}
	writeJSON := func(t wire.FrameType, v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return wire.WriteJSON(conn, t, v)
	}

	// reader: daemon -> stdout. Start before sending any more frames so
	// that net.Pipe() (unbuffered) never deadlocks: the server may write
	// DATA before it reads our RESIZE, so we must have a reader running.
	readerDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		for {
			ft, payload, err := wire.ReadFrame(conn)
			if err != nil {
				return
			}
			switch ft {
			case wire.FrameData:
				_, _ = term.Stdout().Write(payload)
			case wire.FrameEvent, wire.FrameError:
				// Scrollback boundary / errors: ignored for raw passthrough.
			}
		}
	}()

	// Send our current size, then optionally request scrollback replay.
	if cols, rows, err := term.Size(); err == nil && cols > 0 && rows > 0 {
		_ = writeJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
	}
	if opts.RequestReplay {
		_ = writeFrame(wire.FrameRequestReplay, nil)
	}

	// resize: term -> daemon.
	resizeStop := make(chan struct{})
	go func() {
		for {
			select {
			case <-resizeStop:
				return
			case <-term.ResizeEvents():
				if cols, rows, err := term.Size(); err == nil && cols > 0 && rows > 0 {
					_ = writeJSON(wire.FrameResize, wire.Resize{Cols: cols, Rows: rows})
				}
			}
		}
	}()
	defer close(resizeStop)

	// writer: stdin -> daemon, watching for the detach sequence. Runs on
	// this goroutine so a clean detach returns from Attach.
	var scanner DetachScanner
	buf := make([]byte, 4096)
	in := term.Stdin()
	for {
		select {
		case <-readerDone:
			return nil // connection closed (session exited or daemon gone)
		default:
		}
		n, rerr := in.Read(buf)
		for i := 0; i < n; i++ {
			fwd, detach := scanner.Push(buf[i])
			if len(fwd) > 0 {
				if err := writeFrame(wire.FrameData, fwd); err != nil {
					return nil // connection gone
				}
			}
			if detach {
				return nil
			}
		}
		if rerr != nil {
			if errors.Is(rerr, io.EOF) {
				return nil
			}
			return fmt.Errorf("stdin read: %w", rerr)
		}
	}
}
