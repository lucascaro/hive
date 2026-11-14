// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

type fakeLoginItem struct {
	status  string
	setErr  error
	setWith []bool
}

func (f *fakeLoginItem) MenuBarLoginItemStatus() string { return f.status }
func (f *fakeLoginItem) SetMenuBarLoginItem(enable bool) error {
	f.setWith = append(f.setWith, enable)
	if f.setErr == nil {
		if enable {
			f.status = "enabled"
		} else {
			f.status = "not-registered"
		}
	}
	return f.setErr
}

func runLoginItemFlag(t *testing.T, b *fakeLoginItem, args ...string) (handled bool, code int, out, errOut string) {
	t.Helper()
	var so, se bytes.Buffer
	handled, code = handleMenuBarLoginItemFlag(args, &so, &se, b)
	return handled, code, so.String(), se.String()
}

func TestLoginItemFlagStatusPrintsBackendValue(t *testing.T) {
	b := &fakeLoginItem{status: "requires-approval"}
	handled, code, out, _ := runLoginItemFlag(t, b, "--menubar-login-item=status")
	if !handled || code != 0 {
		t.Fatalf("handled=%v code=%d, want true/0", handled, code)
	}
	if out != "hive-login-item: requires-approval\n" {
		t.Errorf("stdout = %q", out)
	}
	if len(b.setWith) != 0 {
		t.Errorf("status must not mutate; Set called with %v", b.setWith)
	}
}

func TestLoginItemFlagEnableDisableCallBackendAndReportResult(t *testing.T) {
	b := &fakeLoginItem{status: "not-registered"}
	_, code, out, _ := runLoginItemFlag(t, b, "--menubar-login-item=enable")
	if code != 0 || out != "hive-login-item: enabled\n" {
		t.Errorf("enable: code=%d out=%q", code, out)
	}
	_, code, out, _ = runLoginItemFlag(t, b, "--menubar-login-item=disable")
	if code != 0 || out != "hive-login-item: not-registered\n" {
		t.Errorf("disable: code=%d out=%q", code, out)
	}
	if len(b.setWith) != 2 || !b.setWith[0] || b.setWith[1] {
		t.Errorf("Set calls = %v, want [true false]", b.setWith)
	}
}

func TestLoginItemFlagErrorGoesToStderrExit1(t *testing.T) {
	b := &fakeLoginItem{status: "not-registered", setErr: errors.New("Invalid argument (SMAppServiceErrorDomain 1)")}
	handled, code, out, errOut := runLoginItemFlag(t, b, "--menubar-login-item=enable")
	if !handled || code != 1 {
		t.Fatalf("handled=%v code=%d, want true/1", handled, code)
	}
	if out != "" {
		t.Errorf("a failed set must print no sentinel, got %q", out)
	}
	if errOut != "Invalid argument (SMAppServiceErrorDomain 1)\n" {
		t.Errorf("stderr = %q, want the backend error verbatim", errOut)
	}
}

func TestLoginItemFlagRejectsUnknownVerb(t *testing.T) {
	b := &fakeLoginItem{}
	handled, code, out, _ := runLoginItemFlag(t, b, "--menubar-login-item=bogus")
	if !handled || code != 2 || out != "" {
		t.Errorf("handled=%v code=%d out=%q, want true/2/empty", handled, code, out)
	}
}

func TestLoginItemFlagIgnoresOtherArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"--version"}, {"--menubar-login-item=status", "extra"}} {
		b := &fakeLoginItem{}
		if handled, _, _, _ := runLoginItemFlag(t, b, args...); handled {
			t.Errorf("args %v were handled; normal startup must proceed", args)
		}
	}
}

// The flag is only side-effect free if it runs before the log tee, the
// menubar.Spawn and the Wails window. A fake backend cannot see main() being
// reordered, so read main.go itself.
func TestLoginItemFlagRunsBeforeStartup(t *testing.T) {
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var mainFn *ast.FuncDecl
	for _, d := range f.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok && fd.Name.Name == "main" && fd.Recv == nil {
			mainFn = fd
		}
	}
	if mainFn == nil {
		t.Fatal("no func main in main.go")
	}
	pos := map[string]token.Pos{}
	ast.Inspect(mainFn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		var name string
		switch fn := call.Fun.(type) {
		case *ast.Ident:
			name = fn.Name
		case *ast.SelectorExpr:
			if x, ok := fn.X.(*ast.Ident); ok {
				name = x.Name + "." + fn.Sel.Name
			}
		}
		// The flag call counts at its LAST occurrence and everything else
		// at its first, so a stray second call below the startup work
		// cannot hide behind a correct one above it.
		if _, seen := pos[name]; !seen || name == "handleMenuBarLoginItemFlag" {
			pos[name] = call.Pos()
		}
		return true
	})
	flag, ok := pos["handleMenuBarLoginItemFlag"]
	if !ok {
		t.Fatal("main() never calls handleMenuBarLoginItemFlag")
	}
	for _, later := range []string{"setupLogFile", "logLaunch", "menubar.Spawn", "wails.Run"} {
		p, ok := pos[later]
		if !ok {
			t.Fatalf("main() no longer calls %s; update this test", later)
		}
		if flag > p {
			t.Errorf("handleMenuBarLoginItemFlag runs after %s; a status probe would start hivebar/open a window", later)
		}
	}
}
