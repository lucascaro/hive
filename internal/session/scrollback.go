package session

import "sync"

// Scrollback is an in-memory ring buffer of recent PTY output bytes.
// It is the cheap, fast layer of scrollback. The disk-backed log
// (see persist.go) is the durable layer.
//
// The ring is byte-based, not line-based, because PTY output is a
// stream of escape sequences and reflowing on line boundaries would
// corrupt cursor positioning on replay. We trade simplicity for
// fidelity here.
type Scrollback struct {
	mu   sync.Mutex
	buf  []byte
	cap  int
}

// NewScrollback returns a ring of the given byte capacity.
func NewScrollback(capacity int) *Scrollback {
	if capacity <= 0 {
		capacity = 256 << 10 // 256 KiB default
	}
	return &Scrollback{cap: capacity}
}

// Write appends bytes to the ring, dropping the oldest bytes once the
// capacity is reached. Always returns len(p), nil — drops are silent
// by design.
func (s *Scrollback) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) >= s.cap {
		// New chunk by itself overruns capacity; keep only its tail.
		s.buf = append(s.buf[:0], p[len(p)-s.cap:]...)
		return len(p), nil
	}
	if len(s.buf)+len(p) <= s.cap {
		s.buf = append(s.buf, p...)
		return len(p), nil
	}
	// Shift out the oldest bytes to make room.
	overflow := len(s.buf) + len(p) - s.cap
	s.buf = append(s.buf[overflow:], p...)
	return len(p), nil
}

// Snapshot returns a copy of the current ring contents. Safe to retain.
func (s *Scrollback) Snapshot() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]byte, len(s.buf))
	copy(out, s.buf)
	return out
}

// Len returns the current number of bytes buffered.
func (s *Scrollback) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.buf)
}
