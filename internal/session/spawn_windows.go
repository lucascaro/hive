//go:build windows

package session

import (
	"syscall"

	"github.com/aymanbagabas/go-pty"
)

// newWindowsCmd builds a `*pty.Cmd` that spawns `wrapper` (cmd.exe) with
// the command-line `wrapper /S /C "<line>"`, bypassing Go's automatic
// argv re-quoting. We need the bypass because:
//
//   - The PTY backend (`go-pty`) on Windows runs each argv element through
//     `windows.ComposeCommandLine` → `EscapeArg`, which wraps any arg
//     containing whitespace in additional quotes and backslash-escapes any
//     embedded `"`. The precisely-quoted line produced by `cmdExeEscape`
//     contains both, so without the bypass we end up with mangled output
//     like `\"cmd.exe\" \"/C\" \"echo hivetest\"` reaching cmd.exe.
//
//   - `syscall.SysProcAttr.CmdLine`, when non-empty, is used verbatim as
//     `lpCommandLine` for CreateProcessW — exactly what we want.
//
// The `/S /C "<line>"` shape is the canonical safe pattern: `/S` makes
// cmd.exe deterministically strip exactly one outer pair of quotes,
// leaving the per-argument quoting in `<line>` intact for every argv
// shape (single token, multi token, args with metacharacters).
func newWindowsCmd(ptmx pty.Pty, wrapper, line string) *pty.Cmd {
	c := ptmx.Command(wrapper)
	// Path is set by ptmx.Command via lookExtensions; preserve it.
	// Args[0] is also wrapper. Replace the command-line entirely so
	// neither EscapeArg nor ComposeCommandLine touches our quoting.
	c.SysProcAttr = &syscall.SysProcAttr{
		CmdLine: wrapper + ` /S /C "` + line + `"`,
	}
	return c
}
