package main

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

type App struct {
	ctx context.Context

	mu   sync.Mutex
	ptmx *os.File
	cmd  *exec.Cmd
}

func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

func (a *App) shutdown(ctx context.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ptmx != nil {
		_ = a.ptmx.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
	}
}

// StartShell spawns the shell on a PTY at the requested initial size.
// The frontend calls this after xterm.js mounts and reports cols/rows.
func (a *App) StartShell(cols, rows int) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.ptmx != nil {
		return nil // idempotent (HMR / double-call safe)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/bash"
	}
	cmd := exec.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	ws := &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)}
	ptmx, err := pty.StartWithSize(cmd, ws)
	if err != nil {
		return err
	}
	a.ptmx = ptmx
	a.cmd = cmd
	go a.ptyReadLoop(ptmx)
	return nil
}

func (a *App) ptyReadLoop(ptmx *os.File) {
	buf := make([]byte, 4096)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			// Base64 keeps the transport binary-safe over JSON-encoded events.
			wruntime.EventsEmit(a.ctx, "pty:data", base64.StdEncoding.EncodeToString(buf[:n]))
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				wruntime.EventsEmit(a.ctx, "pty:exit", "eof")
			} else {
				wruntime.EventsEmit(a.ctx, "pty:exit", err.Error())
			}
			return
		}
	}
}

// WriteStdin accepts base64-encoded bytes from xterm.js's onData handler.
func (a *App) WriteStdin(b64 string) error {
	data, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return err
	}
	a.mu.Lock()
	ptmx := a.ptmx
	a.mu.Unlock()
	if ptmx == nil {
		return nil
	}
	_, err = ptmx.Write(data)
	return err
}

// Resize forwards xterm.js's computed dimensions to the PTY (TIOCSWINSZ).
func (a *App) Resize(cols, rows int) error {
	a.mu.Lock()
	ptmx := a.ptmx
	a.mu.Unlock()
	if ptmx == nil {
		return nil
	}
	return pty.Setsize(ptmx, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
}
