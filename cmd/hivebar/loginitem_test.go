// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

//go:build darwin

package main

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeRun is a scripted runner that records every call.
type fakeRun struct {
	mu    sync.Mutex
	calls []string
	fn    func(name string, args []string) (string, error)
}

func (f *fakeRun) run(_ time.Duration, name string, args ...string) (string, error) {
	f.mu.Lock()
	f.calls = append(f.calls, name+" "+strings.Join(args, " "))
	f.mu.Unlock()
	return f.fn(name, args)
}

func (f *fakeRun) got() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func sentinel(status string) string { return loginItemSentinel + status + "\n" }

func TestLoginItemViewMatrix(t *testing.T) {
	cases := []struct {
		status  string
		err     error
		visible bool
		checked bool
		title   string
	}{
		{"enabled", nil, true, true, "Start at Login"},
		{"not-registered", nil, true, false, "Start at Login"},
		{"requires-approval", nil, true, false, "Start at Login — Approve in System Settings…"},
		{"not-found", nil, false, false, ""},
		{"unsupported", nil, false, false, ""},
		{"", nil, false, false, ""},
		{"enabled", errors.New("probe failed"), false, false, ""},
	}
	for _, c := range cases {
		v := loginItemView(c.status, c.err)
		if v.Visible != c.visible || v.Checked != c.checked || v.Title != c.title {
			t.Errorf("view(%q, %v) = %+v", c.status, c.err, v)
		}
	}
}

func TestParseLoginStatusRequiresTheSentinel(t *testing.T) {
	if s, err := parseLoginStatus("noise\n" + sentinel("enabled")); err != nil || s != "enabled" {
		t.Errorf("got %q, %v", s, err)
	}
	for _, out := range []string{"", "enabled\n", loginItemSentinel + "\n", "hivegui 2.10.0\n"} {
		if _, err := parseLoginStatus(out); err == nil {
			t.Errorf("parseLoginStatus(%q) succeeded; unknown output must be a failed probe", out)
		}
	}
}

func TestToggleLoginItemDisablesWhenOnAndEnablesWhenOff(t *testing.T) {
	f := &fakeRun{fn: func(_ string, args []string) (string, error) {
		switch args[0] {
		case loginItemFlag + "disable":
			return sentinel("not-registered"), nil
		case loginItemFlag + "enable":
			return sentinel("enabled"), nil
		}
		return "", errors.New("unexpected " + args[0])
	}}
	if got, err := toggleLoginItem(f.run, "/gui", "enabled"); err != nil || got != "not-registered" {
		t.Errorf("enabled click: %q, %v", got, err)
	}
	if got, err := toggleLoginItem(f.run, "/gui", "not-registered"); err != nil || got != "enabled" {
		t.Errorf("not-registered click: %q, %v", got, err)
	}
	want := []string{"/gui " + loginItemFlag + "disable", "/gui " + loginItemFlag + "enable"}
	if got := f.got(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("calls = %v, want %v", got, want)
	}
}

// Calling enable again on a registration that is waiting for approval does
// nothing the user can see. The click has to take them to where it is granted.
func TestRequiresApprovalClickOpensSystemSettings(t *testing.T) {
	f := &fakeRun{fn: func(name string, args []string) (string, error) {
		if name == "open" {
			return "", nil
		}
		return sentinel("requires-approval"), nil
	}}
	got, err := toggleLoginItem(f.run, "/gui", "requires-approval")
	if err != nil || got != "requires-approval" {
		t.Errorf("got %q, %v", got, err)
	}
	calls := f.got()
	if len(calls) != 2 || calls[0] != "open "+loginItemsSettingsURL {
		t.Errorf("calls = %v, want open <Login Items URL> then a re-probe", calls)
	}
	for _, c := range calls {
		if strings.Contains(c, "enable") {
			t.Errorf("requires-approval must not call enable: %v", calls)
		}
	}
}

// An unsigned build refuses to register. The menu must report why and the
// checkmark must show what is actually true, not what was clicked.
func TestToggleFailureReturnsErrorAndRereadsStatus(t *testing.T) {
	f := &fakeRun{fn: func(_ string, args []string) (string, error) {
		if args[0] == loginItemFlag+"enable" {
			return "", errors.New("Invalid argument (SMAppServiceErrorDomain 1)")
		}
		return sentinel("not-registered"), nil
	}}
	got, err := toggleLoginItem(f.run, "/gui", "not-registered")
	if err == nil || !strings.Contains(err.Error(), "Invalid argument") {
		t.Fatalf("err = %v, want the stderr text", err)
	}
	if got != "not-registered" {
		t.Errorf("status after failure = %q, want the re-read truth", got)
	}
	if len(f.got()) != 2 {
		t.Errorf("calls = %v, want the enable attempt then a re-probe", f.got())
	}
}

func TestProbeFailureHidesItem(t *testing.T) {
	f := &fakeRun{fn: func(string, []string) (string, error) { return "hivegui 2.9.0 (older, ignores the flag)\n", nil }}
	status, err := probeLoginItem(f.run, "/gui")
	if err == nil {
		t.Fatalf("probe of a hivegui that ignores the flag succeeded with %q", status)
	}
	if v := loginItemView(status, err); v.Visible {
		t.Errorf("a failed probe left the item visible: %+v", v)
	}
}

func TestToggleLoginItemRefusesUnknownStatus(t *testing.T) {
	f := &fakeRun{fn: func(string, []string) (string, error) { return "", nil }}
	if _, err := toggleLoginItem(f.run, "/gui", "not-found"); err == nil {
		t.Error("toggling a login item the OS cannot see must fail")
	}
	if len(f.got()) != 0 {
		t.Errorf("nothing should run for an unknown status, got %v", f.got())
	}
}
