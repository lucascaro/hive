package muxnative

import (
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

const maxBufSize = 512 * 1024 // 512 KB scrollback buffer

// pane represents a running process attached to a pseudo-terminal.
type pane struct {
	ptm  *os.File // PTY master fd
	cmd  *exec.Cmd
	name string

	mu   sync.Mutex
	buf  []byte // circular output buffer (last maxBufSize bytes)
	dead bool   // true once the process has exited

	// attachWriter receives a copy of all new PTY output while set.
	// Protected by attachMu.
	attachMu     sync.Mutex
	attachWriter io.Writer
}

// startPane creates a PTY, starts cmd in it, and begins buffering output.
func startPane(name, workDir string, args []string) (*pane, error) {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Dir = workDir

	ptm, err := pty.Start(cmd)
	if err != nil {
		return nil, err
	}

	p := &pane{
		ptm:  ptm,
		cmd:  cmd,
		name: name,
		buf:  make([]byte, 0, 4096),
	}
	go p.readLoop()
	return p, nil
}

// readLoop pumps the PTY master into the output buffer and any attached writer.
// It runs until the process exits (PTY read returns an error).
func (p *pane) readLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := p.ptm.Read(buf)
		if n > 0 {
			data := buf[:n]

			p.mu.Lock()
			p.buf = append(p.buf, data...)
			if len(p.buf) > maxBufSize {
				p.buf = p.buf[len(p.buf)-maxBufSize:]
			}
			p.mu.Unlock()

			p.attachMu.Lock()
			w := p.attachWriter
			p.attachMu.Unlock()
			if w != nil {
				w.Write(data) //nolint:errcheck // best-effort
			}
		}
		if err != nil {
			p.mu.Lock()
			p.dead = true
			p.mu.Unlock()
			return
		}
	}
}

// capture returns the last lines of buffered output (all if lines == 0).
func (p *pane) capture(lines int) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return lastLines(string(p.buf), lines)
}

// isDead reports whether the process has exited.
func (p *pane) isDead() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dead
}

// kill terminates the process.
func (p *pane) kill() {
	if p.cmd.Process != nil {
		p.cmd.Process.Kill() //nolint:errcheck
	}
	p.ptm.Close() //nolint:errcheck
}

// resize sets the PTY window size.
func (p *pane) resize(rows, cols uint16) {
	pty.Setsize(p.ptm, &pty.Winsize{Rows: rows, Cols: cols}) //nolint:errcheck
}

// setAttachWriter atomically updates the writer that receives new PTY output.
// Pass nil to disable fan-out.
func (p *pane) setAttachWriter(w io.Writer) {
	p.attachMu.Lock()
	p.attachWriter = w
	p.attachMu.Unlock()
}
