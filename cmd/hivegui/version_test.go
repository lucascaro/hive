// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/buildinfo"
)

func TestPrintVersion(t *testing.T) {
	var buf bytes.Buffer
	if !printVersion(&buf, []string{"--version"}) {
		t.Fatal("--version not handled")
	}
	for _, want := range []string{"hivegui", buildinfo.Copyright, "GPL-3.0-only", buildinfo.Attribution} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("%q missing from %q", want, buf.String())
		}
	}
}

func TestPrintVersionIgnoresOtherArgs(t *testing.T) {
	for _, args := range [][]string{nil, {"--other"}, {"--version", "x"}} {
		var buf bytes.Buffer
		if printVersion(&buf, args) || buf.Len() != 0 {
			t.Errorf("args %v were handled", args)
		}
	}
}

func TestAppOptionsAboutPanelCarriesLegalNotice(t *testing.T) {
	o := appOptions(NewApp(""), 800, 600)
	if o.Mac == nil || o.Mac.About == nil {
		t.Fatal("Mac.About unset; macOS About panel would show no legal notice")
	}
	for _, want := range []string{buildinfo.Copyright, buildinfo.Warranty, buildinfo.Attribution} {
		if !strings.Contains(o.Mac.About.Message, want) {
			t.Errorf("About message missing %q", want)
		}
	}
}
