//go:build !windows

package main

import (
	"io"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"golang.org/x/term"
)

// osTerm implements client.Terminal over the real process stdio.
type osTerm struct {
	resize chan struct{}
}

func newTerm() (*osTerm, func(), error) {
	fd := int(os.Stdin.Fd())
	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return nil, nil, err
	}
	restore := func() { _ = term.Restore(fd, oldState) }

	t := &osTerm{resize: make(chan struct{}, 1)}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGWINCH)
	go func() {
		for range sig {
			select {
			case t.resize <- struct{}{}:
			default:
			}
		}
	}()
	// once guards against a double call (e.g. an explicit cleanup() before
	// os.Exit plus a deferred cleanup()): close on a closed channel panics,
	// and the panic would strand the terminal in raw mode.
	var once sync.Once
	cleanup := func() { once.Do(func() { signal.Stop(sig); close(sig); restore() }) }
	return t, cleanup, nil
}

func (t *osTerm) Stdin() io.Reader              { return os.Stdin }
func (t *osTerm) Stdout() io.Writer             { return os.Stdout }
func (t *osTerm) ResizeEvents() <-chan struct{} { return t.resize }
func (t *osTerm) Size() (int, int, error) {
	return term.GetSize(int(os.Stdout.Fd()))
}
