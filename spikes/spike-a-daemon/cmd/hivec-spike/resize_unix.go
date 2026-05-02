//go:build !windows

package main

import (
	"net"
	"os"
	"os/signal"
	"syscall"
)

func watchResize(conn net.Conn) chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGWINCH)
	go func() {
		for range ch {
			sendResize(conn)
		}
	}()
	return ch
}

func stopResize(ch chan os.Signal) { close(ch) }
