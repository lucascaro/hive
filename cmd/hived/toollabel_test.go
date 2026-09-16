package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// labelVector is one case of testdata/toollabel/vectors.json. The file
// is shared with the Pi extension's TypeScript reporter
// (internal/agent/pi/hive.test.ts), which runs every case too: the
// privacy rule has two implementations, and one table is what keeps them
// from drifting. WantGo overrides Want only for a pinned divergence
// between Go's url.Parse and JS's URL.
type labelVector struct {
	Name   string  `json:"name"`
	Input  any     `json:"input"`
	Want   *string `json:"want"`
	WantGo *string `json:"want_go"`
}

// TestDeriveLabel is the privacy rule's unit-level table. Every case
// that mentions a secret is there because the obvious implementation
// would have leaked it.
func TestDeriveLabel(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "toollabel", "vectors.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Cases []labelVector `json:"cases"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Cases) == 0 {
		t.Fatal("vectors.json has no cases")
	}
	for _, tc := range doc.Cases {
		want := tc.Want
		if tc.WantGo != nil {
			want = tc.WantGo
		}
		if want == nil {
			t.Fatalf("vector %q has neither want nor want_go", tc.Name)
		}
		t.Run(tc.Name, func(t *testing.T) {
			if got := deriveToolTarget(tc.Input); got != *want {
				t.Errorf("deriveToolTarget(%v) = %q, want %q", tc.Input, got, *want)
			}
		})
	}
}

// TestDeriveLabelNonObject: anything that is not an object yields no
// label. Stringifying it here is exactly how raw arguments would leak.
func TestDeriveLabelNonObject(t *testing.T) {
	for _, in := range []any{nil, "a bare string", 42, []any{"a", "list"}} {
		if got := deriveToolTarget(in); got != "" {
			t.Errorf("deriveToolTarget(%v) = %q, want empty", in, got)
		}
	}
}

// TestDeriveLabelCapped: an over-long label means the derivation
// over-captured, so the cap is a leak backstop as much as a size limit.
func TestDeriveLabelCapped(t *testing.T) {
	long := "/tmp/" + strings.Repeat("n", 500) + ".txt"
	got := deriveToolTarget(map[string]any{"file_path": long})
	if len(got) != wire.MaxTargetLen {
		t.Errorf("len = %d, want %d", len(got), wire.MaxTargetLen)
	}
}

// TestCapLabelRuneBoundary: capping must not split a rune — the label
// is agent-authored and can be any script.
func TestCapLabelRuneBoundary(t *testing.T) {
	// 3 bytes per rune, so the cap lands mid-rune unless handled.
	s := strings.Repeat("世", wire.MaxTargetLen)
	got := capLabel(s)
	if len(got) > wire.MaxTargetLen {
		t.Fatalf("len = %d, want <= %d", len(got), wire.MaxTargetLen)
	}
	for _, r := range got {
		if r == '\uFFFD' {
			t.Fatalf("capLabel split a rune: %q", got)
		}
	}
}

// TestIsSubcommandKnownCeiling pins the heuristic's documented limit,
// so the day someone tightens it (to a subcommand allowlist) the test
// that changes is this one and the decision is visible. A short,
// all-letter secret with at most one separator is indistinguishable
// from a subcommand by shape alone.
func TestIsSubcommandKnownCeiling(t *testing.T) {
	if !isSubcommand("AbCdEfGhIjKlMnOp") {
		t.Skip("the ceiling was raised — update the ponytail note on isSubcommand and delete this test")
	}
}

// TestIsEnvAssignment pins the shell's rule for a leading NAME=value.
func TestIsEnvAssignment(t *testing.T) {
	for tok, want := range map[string]bool{
		"FOO=bar": true, "_X=1": true, "a1_b=": true, "GITHUB_TOKEN=ghp_x": true,
		"=bar": false, "1FOO=bar": false, "FO-O=bar": false, "foo": false, "": false,
	} {
		if got := isEnvAssignment(tok); got != want {
			t.Errorf("isEnvAssignment(%q) = %v, want %v", tok, got, want)
		}
	}
}

// TestIsSubcommandRejectsNonASCII: the separator and digit checks are
// ASCII, so a token spelled with look-alike runes — U+2010 hyphens,
// U+FF0D fullwidth hyphen-minus, fullwidth digits — slipped past every
// one of them while reading as exactly the credential shape the function
// documents refusing. Real subcommands are ASCII, so any non-ASCII rune
// is refused outright rather than chasing Unicode look-alikes one by one.
func TestIsSubcommandRejectsNonASCII(t *testing.T) {
	for _, tok := range []string{
		"sk‐live‐abc", // HYPHEN
		"sk－live－abc", // FULLWIDTH HYPHEN-MINUS
		"key１２３",      // FULLWIDTH DIGITS
		"tést",        // a plain accented word — refused too; the cost of the rule
	} {
		if isSubcommand(tok) {
			t.Errorf("isSubcommand(%q) = true, want false", tok)
		}
	}
	if got := deriveToolTarget(map[string]any{"command": "mytool sk‐live‐abc"}); got != "mytool" {
		t.Errorf("label = %q, want %q", got, "mytool")
	}
}

// TestCommandWordCredentialCeiling pins a documented, operator-accepted
// leak: the command word gets no credential-shape check, so a secret typed
// AS the command becomes the label. If this starts failing, someone closed
// the gap — update the ponytail note on commandHead and delete this test.
func TestCommandWordCredentialCeiling(t *testing.T) {
	if got := deriveToolTarget(map[string]any{"command": "sk-live-abcdefghij run"}); got != "sk-live-abcdefghij run" {
		t.Skipf("the ceiling was raised (label = %q) — update the ponytail note on commandHead and delete this test", got)
	}
}
