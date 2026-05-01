// hived-spike is the throwaway daemon for Phase 0 Spike A.
//
// It owns one shell on a PTY. A single client at a time can attach
// over a Unix socket; the daemon survives client disconnects, keeps
// draining the PTY into a small ring buffer, and replays the buffer
// to whichever client attaches next.
//
// This code is intentionally throwaway. Phase 1 will redesign the
// daemon, protocol, persistence, and lifecycle from scratch using
// the lessons documented in docs/native-rewrite/phase-0-report.md.
package main

import (
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"

	"github.com/aymanbagabas/go-pty"
	"github.com/lucascaro/hive/spikes/spike-a-daemon/internal/proto"
)

const ringSize = 4096

type ringBuffer struct {
	mu  sync.Mutex
	buf []byte
}

func (r *ringBuffer) Write(p []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(p) >= ringSize {
		r.buf = append(r.buf[:0], p[len(p)-ringSize:]...)
		return
	}
	if len(r.buf)+len(p) <= ringSize {
		r.buf = append(r.buf, p...)
		return
	}
	overflow := len(r.buf) + len(p) - ringSize
	r.buf = append(r.buf[overflow:], p...)
}

func (r *ringBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]byte, len(r.buf))
	copy(out, r.buf)
	return out
}

type daemon struct {
	ptmx pty.Pty
	ring *ringBuffer

	mu     sync.Mutex
	client net.Conn
}

// swapClient replaces the active client and returns the previous one
// (which the caller is responsible for closing).
func (d *daemon) swapClient(c net.Conn) net.Conn {
	d.mu.Lock()
	defer d.mu.Unlock()
	old := d.client
	d.client = c
	return old
}

func (d *daemon) clearClient(c net.Conn) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.client == c {
		d.client = nil
	}
}

func (d *daemon) sendToClient(t byte, data []byte) {
	d.mu.Lock()
	c := d.client
	d.mu.Unlock()
	if c == nil {
		return
	}
	if err := proto.WriteFrame(c, t, data); err != nil {
		_ = c.Close()
		d.clearClient(c)
	}
}

func (d *daemon) ptyReadLoop() {
	buf := make([]byte, 4096)
	for {
		n, err := d.ptmx.Read(buf)
		if n > 0 {
			d.ring.Write(buf[:n])
			d.sendToClient(proto.FrameData, buf[:n])
		}
		if err != nil {
			if !errors.Is(err, io.EOF) {
				log.Printf("pty read: %v", err)
			}
			return
		}
	}
}

func (d *daemon) handleClient(c net.Conn) {
	defer func() {
		_ = c.Close()
		d.clearClient(c)
	}()

	if snap := d.ring.Snapshot(); len(snap) > 0 {
		if err := proto.WriteFrame(c, proto.FrameData, snap); err != nil {
			return
		}
	}

	for {
		t, data, err := proto.ReadFrame(c)
		if err != nil {
			return
		}
		switch t {
		case proto.FrameData:
			if _, err := d.ptmx.Write(data); err != nil {
				return
			}
		case proto.FrameResize:
			cols, rows, ok := proto.DecodeResize(data)
			if !ok {
				continue
			}
			_ = d.ptmx.Resize(int(cols), int(rows))
		}
	}
}

func defaultShell() string {
	if s := os.Getenv("SHELL"); s != "" {
		return s
	}
	if s := os.Getenv("ComSpec"); s != "" {
		return s // Windows fallback
	}
	if _, err := os.Stat("/bin/bash"); err == nil {
		return "/bin/bash"
	}
	return "cmd.exe"
}

func main() {
	var shellFlag string
	flag.StringVar(&shellFlag, "shell", "", "shell to run (defaults to $SHELL, $ComSpec, or /bin/bash)")
	flag.Parse()

	shell := shellFlag
	if shell == "" {
		shell = defaultShell()
	}

	ptmx, err := pty.New()
	if err != nil {
		log.Fatalf("pty new: %v", err)
	}
	defer func() { _ = ptmx.Close() }()

	cmd := ptmx.Command(shell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if err := cmd.Start(); err != nil {
		log.Fatalf("cmd start: %v", err)
	}

	d := &daemon{ptmx: ptmx, ring: &ringBuffer{}}
	go d.ptyReadLoop()

	sock := proto.SocketPath(os.Getuid())
	_ = os.MkdirAll(filepath.Dir(sock), 0o755)
	_ = os.Remove(sock)
	ln, err := net.Listen("unix", sock)
	if err != nil {
		log.Fatalf("listen %s: %v", sock, err)
	}
	log.Printf("hived-spike: listening on %s (pid %d, shell %s)", sock, os.Getpid(), shell)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		log.Printf("hived-spike: shutting down")
		_ = ln.Close()
		_ = ptmx.Close()
		_ = os.Remove(sock)
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		os.Exit(0)
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			log.Printf("accept: %v", err)
			return
		}
		log.Printf("client attached: %s", conn.RemoteAddr())
		if old := d.swapClient(conn); old != nil {
			_ = old.Close()
		}
		go d.handleClient(conn)
	}
}
