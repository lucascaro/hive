//go:build windows

package main

import (
	"net"
	"os"
	"time"

	"golang.org/x/term"
)

// Windows has no SIGWINCH. For the spike we poll the console size at
// 250ms and emit a resize frame on change. Good enough to validate the
// pipeline; production code should use SetConsoleMode + a window-resize
// event from kernel32.
func watchResize(conn net.Conn) chan os.Signal {
	ch := make(chan os.Signal, 1) // unused on Windows; kept for API parity
	go func() {
		fd := int(os.Stdout.Fd())
		lastCols, lastRows, _ := term.GetSize(fd)
		t := time.NewTicker(250 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ch:
				return
			case <-t.C:
				cols, rows, err := term.GetSize(fd)
				if err != nil {
					continue
				}
				if cols != lastCols || rows != lastRows {
					lastCols, lastRows = cols, rows
					sendResize(conn)
				}
			}
		}
	}()
	return ch
}

func stopResize(ch chan os.Signal) { close(ch) }
