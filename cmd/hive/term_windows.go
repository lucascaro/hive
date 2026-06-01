//go:build windows

package main

import (
	"io"
	"os"
	"sync"
	"time"

	"golang.org/x/term"
)

// osTerm implements client.Terminal over the real process stdio.
// Windows has no SIGWINCH, so size changes are detected by polling.
type osTerm struct {
	resize chan struct{}
	stop   chan struct{}
}

func newTerm() (*osTerm, func(), error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	restore := func() { _ = term.Restore(fd, oldState) }

	t := &osTerm{resize: make(chan struct{}, 1), stop: make(chan struct{})}
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		lastC, lastR, _ := t.Size()
		for {
			select {
			case <-t.stop:
				return
			case <-ticker.C:
				c, r, err := t.Size()
				if err == nil && (c != lastC || r != lastR) {
					lastC, lastR = c, r
					select {
					case t.resize <- struct{}{}:
					default:
					}
				}
			}
		}
	}()
	// once guards against a double call (e.g. an explicit cleanup() before
	// os.Exit plus a deferred cleanup()): close on a closed channel panics,
	// and the panic would strand the terminal in raw mode.
	var once sync.Once
	cleanup := func() { once.Do(func() { close(t.stop); restore() }) }
	return t, cleanup, nil
}

func (t *osTerm) Stdin() io.Reader              { return os.Stdin }
func (t *osTerm) Stdout() io.Writer             { return os.Stdout }
func (t *osTerm) ResizeEvents() <-chan struct{} { return t.resize }
func (t *osTerm) Size() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}
