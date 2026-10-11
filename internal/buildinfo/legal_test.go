package buildinfo

import (
	"os"
	"strings"
	"testing"
)

func TestLegalNoticeHasEveryLine(t *testing.T) {
	n := LegalNotice()
	for _, want := range []string{Copyright, "GPL-3.0-only", "ABSOLUTELY NO WARRANTY", Attribution} {
		if !strings.Contains(n, want) {
			t.Errorf("LegalNotice missing %q", want)
		}
	}
}

// The GUI carries its own copy of the strings; this fails when they drift.
func TestGUILegalMatchesGo(t *testing.T) {
	b, err := os.ReadFile("../../cmd/hivegui/frontend/src/lib/legal.ts")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{Copyright, Warranty, Attribution} {
		if !strings.Contains(string(b), want) {
			t.Errorf("legal.ts missing %q", want)
		}
	}
}
