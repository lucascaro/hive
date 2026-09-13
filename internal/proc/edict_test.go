package proc_test

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// allowed lists the Windows-buildable files that may call os/exec
// directly, and why. Everything else must go through proc.Command /
// proc.CommandContext — see the package comment for what that buys.
var allowed = map[string]string{
	// The two detached spawns. DETACHED_PROCESS already gives the child
	// no console at all, and CREATE_NO_WINDOW may not be combined with
	// it: CreateProcess fails outright when both are set.
	"cmd/hivegui/window_windows.go": "detached spawn — DETACHED_PROCESS, mutually exclusive with CREATE_NO_WINDOW",
	"cmd/hivegui/spawn_windows.go":  "detached spawn — routes through startDetachedWindows",
	// Opening the OS terminal is the one case where a console window on
	// screen is the point.
	"cmd/hivegui/os_terminal.go": "deliberately opens a visible terminal window",
}

// TestNoDirectExecOnWindows enforces the hard rule in DESIGN.md: in code
// that builds for Windows, child processes are created through
// internal/proc, never os/exec directly. A direct call compiles and
// works, so nothing but this test stops a new spawn site from putting
// console popups back on screen for every Windows user.
//
// Scoped to Windows-buildable files on purpose: the rule exists to stop
// console windows, and files excluded by build constraints on Windows
// cannot open one.
func TestNoDirectExecOnWindows(t *testing.T) {
	root := moduleRoot(t)

	bctx := build.Default
	bctx.GOOS = "windows"
	bctx.GOARCH = "amd64"
	bctx.CgoEnabled = true

	var violations []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		switch d.Name() {
		case ".git", "node_modules", ".worktrees":
			return fs.SkipDir
		}
		pkg, err := bctx.ImportDir(path, 0)
		if err != nil {
			// No buildable Go in this directory for windows/amd64.
			return nil
		}
		files := append(append([]string{}, pkg.GoFiles...), pkg.CgoFiles...)
		for _, name := range files {
			rel := filepath.ToSlash(mustRel(t, root, filepath.Join(path, name)))
			if _, ok := allowed[rel]; ok {
				continue
			}
			if strings.HasPrefix(rel, "internal/proc/") {
				continue // the wrapper itself
			}
			violations = append(violations, scanFile(t, filepath.Join(path, name), rel)...)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}

	sort.Strings(violations)
	if len(violations) > 0 {
		t.Errorf("os/exec called directly in Windows-buildable code (use internal/proc instead — "+
			"a console child spawned from hivegui/hived, neither of which owns a console, "+
			"opens a popup window on every call):\n\t%s", strings.Join(violations, "\n\t"))
	}
}

// scanFile returns "<rel>:<line>: exec.Command" for each direct os/exec
// constructor call in one file.
func scanFile(t *testing.T, path, rel string) []string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", rel, err)
	}

	// The local name of the os/exec import, if the file imports it.
	local := ""
	for _, imp := range f.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil || p != "os/exec" {
			continue
		}
		local = "exec"
		if imp.Name != nil {
			local = imp.Name.Name
		}
	}
	if local == "" || local == "_" {
		return nil
	}

	var out []string
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Name != local {
			return true
		}
		if sel.Sel.Name != "Command" && sel.Sel.Name != "CommandContext" {
			return true
		}
		out = append(out, rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line)+
			": "+local+"."+sel.Sel.Name)
		return true
	})
	return out
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test's working directory")
		}
		dir = parent
	}
}

func mustRel(t *testing.T, base, target string) string {
	t.Helper()
	rel, err := filepath.Rel(base, target)
	if err != nil {
		t.Fatalf("rel %s %s: %v", base, target, err)
	}
	return rel
}
