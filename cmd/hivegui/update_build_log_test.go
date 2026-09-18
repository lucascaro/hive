//go:build darwin || windows

package main

import (
	"strings"
	"testing"
)

// The log viewer renders text, so colour escapes and CR redraws must be
// gone; and the one-line summary must skip Wails' sponsorship trailer,
// which it prints after every build — failed ones included.
func TestStreamBuildOutputStripsANSIAndSkipsSponsorTrailer(t *testing.T) {
	out := strings.Join([]string{
		"==> [macos] Building Wails universal .app",
		"\x1b[31mERROR\x1b[0m  xcrun: error: license not accepted",
		"progress 10%\rprogress 100%",
		"",
		"\x1b[31;107m ♥ \x1b[0m \x1b[92mIf Wails is useful to you or your company, please consider sponsoring the project:\x1b[0m",
		"https://github.com/sponsors/leaanthony",
	}, "\n")
	var progress []string
	log, summary := streamBuildOutput(strings.NewReader(out), func(s string) { progress = append(progress, s) })

	if strings.ContainsRune(log, 0x1b) || strings.ContainsRune(log, '\r') {
		t.Errorf("log still has control bytes: %q", log)
	}
	if !strings.Contains(log, "ERROR  xcrun: error: license not accepted") {
		t.Errorf("log lost the error line: %q", log)
	}
	if !strings.Contains(log, "sponsors/leaanthony") {
		t.Error("log dropped the trailer; the full log should keep everything")
	}
	if strings.Contains(log, "progress 10%") {
		t.Error("log kept text a CR redraw overwrote")
	}
	if summary != "progress 100%" {
		t.Errorf("summary = %q, want the last line that is not the Wails trailer", summary)
	}
	if len(progress) == 0 || progress[len(progress)-1] != "https://github.com/sponsors/leaanthony" {
		t.Errorf("progress = %q, want every non-empty line relayed", progress)
	}
}

func TestStreamBuildOutputKeepsTheTailWhenCapped(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3*maxBuildLogLines; i++ {
		b.WriteString("x\n")
	}
	b.WriteString("the failure")
	log, _ := streamBuildOutput(strings.NewReader(b.String()), func(string) {})
	lines := strings.Split(log, "\n")
	if len(lines) != maxBuildLogLines {
		t.Errorf("kept %d lines, want %d", len(lines), maxBuildLogLines)
	}
	if lines[len(lines)-1] != "the failure" {
		t.Errorf("last kept line = %q, want the tail", lines[len(lines)-1])
	}
}
