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
	violations := scanTree(t, moduleRoot(t))

	sort.Strings(violations)
	if len(violations) > 0 {
		t.Errorf("os/exec called directly in Windows-buildable code (use internal/proc instead — "+
			"a console child spawned from hivegui/hived, neither of which owns a console, "+
			"opens a popup window on every call):\n\t%s", strings.Join(violations, "\n\t"))
	}
}

// scanTree walks root and returns every direct os/exec constructor call
// in the Go files that build for windows/amd64.
//
// Split out of TestNoDirectExecOnWindows so the walk can be exercised
// against a fixture tree, not only against this module.
func scanTree(t *testing.T, root string) []string {
	t.Helper()

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
		// A directory carrying a .git entry is a checkout of its own - a
		// clone, or a worktree (Claude Code puts them under
		// .claude/worktrees). Its files belong to another branch and are
		// not this one's to police. Keying on .git covers all of those in
		// one rule, rather than growing the name list above forever.
		if path != root {
			if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
				return fs.SkipDir
			}
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
	return violations
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
		// A dot import binds the constructors as bare names, so the call
		// is an *ast.Ident and never an *ast.SelectorExpr. Matching only
		// selectors let a file dot-import its way past the edict.
		var name string
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			id, ok := fun.X.(*ast.Ident)
			if !ok || id.Name != local {
				return true
			}
			name = fun.Sel.Name
		case *ast.Ident:
			if local != "." {
				return true
			}
			name = fun.Name
		default:
			return true
		}
		if name != "Command" && name != "CommandContext" {
			return true
		}
		label := local + "." + name
		if local == "." {
			label = name + " (dot-imported os/exec)"
		}
		out = append(out, rel+":"+strconv.Itoa(fset.Position(call.Pos()).Line)+
			": "+label)
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

// Fixtures for the scanner tests below.
const (
	selectorViolation = `package bad

import "os/exec"

func run() { _ = exec.Command("git") }
`
	dotImportViolation = `package dot

import . "os/exec"

func run() { _ = Command("git") }
`
)

// writeGoFile puts src at path, creating parent directories.
func writeGoFile(t *testing.T, path, src string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A checkout nested inside the repo holds another branch's code, which
// this branch cannot fix. Claude Code creates them under
// .claude/worktrees/<name>, and stray clones happen too. Scanning them
// turns `go test ./...` permanently red on a working checkout with
// violations nobody can act on - and unreadable noise is how an edict
// test gets ignored. CI never saw it because CI clones fresh.
//
// The nested marker is a .git directory for a clone and a .git file for
// a worktree, so both have to be skipped.
func TestScanTreeSkipsNestedCheckouts(t *testing.T) {
	for _, tc := range []struct {
		name  string
		asDir bool
	}{
		{"nested clone, .git directory", true},
		{"git worktree, .git file", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()

			// Part of the tree under test: must still be reported, so a
			// skip that swallowed everything could not pass this test.
			writeGoFile(t, filepath.Join(root, "pkg", "bad.go"), selectorViolation)

			nested := filepath.Join(root, ".claude", "worktrees", "other-branch")
			writeGoFile(t, filepath.Join(nested, "bad.go"), selectorViolation)
			if tc.asDir {
				if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o755); err != nil {
					t.Fatal(err)
				}
			} else {
				writeGoFile(t, filepath.Join(nested, ".git"),
					"gitdir: /elsewhere/.git/worktrees/other-branch\n")
			}

			got := scanTree(t, root)
			if len(got) != 1 {
				t.Fatalf("scanTree = %d violations, want exactly the one in pkg/:\n\t%s",
					len(got), strings.Join(got, "\n\t"))
			}
			if !strings.HasPrefix(got[0], "pkg/bad.go:") {
				t.Errorf("scanTree = %q, want the violation in pkg/bad.go", got[0])
			}
		})
	}
}

// `import . "os/exec"` binds Command as a bare identifier, so the call
// is an *ast.Ident and never an *ast.SelectorExpr. A file could dot-
// import its way straight past the edict.
func TestScanFileCatchesDotImportedExec(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dot.go")
	writeGoFile(t, path, dotImportViolation)

	got := scanFile(t, path, "dot.go")
	if len(got) != 1 {
		t.Fatalf("scanFile = %d violations for a dot-imported exec.Command, want 1: %v", len(got), got)
	}
}
