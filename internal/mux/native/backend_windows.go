//go:build windows

// Package muxnative is the native PTY multiplexer backend. On Windows this
// backend is not available because it relies on Unix pseudo-terminals. All
// methods return a descriptive error; IsAvailable returns false so the
// configuration layer will never auto-select this backend.
//
// Windows users should set multiplexer to "tmux" in config and install tmux
// via WSL, MSYS2, or Chocolatey.
package muxnative

import (
	"errors"

	"github.com/lucascaro/hive/internal/mux"
)

var errNotSupported = errors.New(
	"native PTY backend is not supported on Windows — " +
		"set `multiplexer: tmux` in your config and install tmux via WSL, MSYS2, or Chocolatey",
)

// Backend is a no-op implementation of mux.Backend for Windows.
type Backend struct{}

// NewBackend returns a Backend stub. EnsureRunning need not be called on Windows.
func NewBackend(_ string) *Backend { return &Backend{} }

// IsAvailable always returns false on Windows.
func (b *Backend) IsAvailable() bool { return false }

// IsServerRunning always returns false on Windows.
func (b *Backend) IsServerRunning() bool { return false }

func (b *Backend) CreateSession(_, _, _ string, _ []string) error         { return errNotSupported }
func (b *Backend) SessionExists(_ string) bool                             { return false }
func (b *Backend) KillSession(_ string) error                              { return errNotSupported }
func (b *Backend) ListSessionNames() ([]string, error)                    { return nil, errNotSupported }
func (b *Backend) CreateWindow(_, _, _ string, _ []string) (int, error)   { return 0, errNotSupported }
func (b *Backend) WindowExists(_ string) bool                              { return false }
func (b *Backend) KillWindow(_ string) error                               { return errNotSupported }
func (b *Backend) RenameWindow(_, _ string) error                          { return errNotSupported }
func (b *Backend) ListWindows(_ string) ([]mux.WindowInfo, error)         { return nil, errNotSupported }
func (b *Backend) CapturePane(_ string, _ int) (string, error)            { return "", errNotSupported }
func (b *Backend) CapturePaneRaw(_ string, _ int) (string, error)         { return "", errNotSupported }
func (b *Backend) GetCurrentCommand(_ string) (string, error)             { return "", errNotSupported }
func (b *Backend) Attach(_ string) error                                   { return errNotSupported }
func (b *Backend) DetachKey() string                                       { return "" }
func (b *Backend) SupportsPopup() bool                                     { return false }
func (b *Backend) PopupAttach(_, _ string) error                           { return errNotSupported }
func (b *Backend) UseExecAttach() bool                                     { return false }

// SockPath returns an empty string on Windows (no socket used).
func SockPath() string { return "" }

// EnsureRunning is a no-op on Windows.
func EnsureRunning(_, _ string) error { return nil }

// Ping always returns errNotSupported on Windows.
func Ping(_ string) error { return errNotSupported }

// RunDaemon is a no-op on Windows; the native daemon cannot run without Unix PTYs.
func RunDaemon(_, _ string) error { return errNotSupported }
