// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The menu is a fixed pool: every item is created once, in Ready, and only
// retitled afterwards. systray.AddMenuItem APPENDS, so an item added later
// lands below the footer and the menu visibly reorders itself (see the Menu
// type comment). Scan every production file, not just menu.go, so a new
// file cannot slip an Add past this.
func TestAddMenuItemOnlyInsideReady(t *testing.T) {
	adders := map[string]bool{
		"AddMenuItem": true, "AddMenuItemCheckbox": true, "AddSeparator": true,
		"AddSubMenuItem": true, "AddSubMenuItemCheckbox": true,
	}
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v (%d files)", err, len(files))
	}
	sawReady := false
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		f, err := parser.ParseFile(token.NewFileSet(), name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, d := range f.Decls {
			fd, ok := d.(*ast.FuncDecl)
			if !ok || fd.Body == nil {
				continue
			}
			inReady := fd.Name.Name == "Ready" && fd.Recv != nil
			sawReady = sawReady || inReady
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				call, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && adders[sel.Sel.Name] && !inReady {
					t.Errorf("%s: %s called in %s; items may only be created in (*Menu).Ready",
						name, sel.Sel.Name, fd.Name.Name)
				}
				return true
			})
		}
	}
	if !sawReady {
		t.Error("never found (*Menu).Ready; the scan is not looking at the menu")
	}
}
