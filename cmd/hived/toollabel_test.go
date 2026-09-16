package main

import (
	"strings"
	"testing"

	"github.com/lucascaro/hive/internal/wire"
)

// TestDeriveLabel is the privacy rule's unit-level table. Every case
// that mentions a secret is there because the obvious implementation
// would have leaked it.
func TestDeriveLabel(t *testing.T) {
	cases := []struct {
		name  string
		input map[string]any
		want  string
	}{
		// --- paths ---
		{"unix path", map[string]any{"file_path": "/Users/dev/hive/internal/agentstate/machine.go"}, "machine.go"},
		{
			// The spec's separator-agnostic criterion. filepath.Base on
			// a unix build does NOT split this, which is the bug.
			"windows path",
			map[string]any{"file_path": `C:\Users\dev\hive\internal\agentstate\machine.go`},
			"machine.go",
		},
		{"bare filename", map[string]any{"path": "README.md"}, "README.md"},
		{"notebook", map[string]any{"notebook_path": "/tmp/analysis/run.ipynb"}, "run.ipynb"},

		// --- commands ---
		{"single token", map[string]any{"command": "ls"}, "ls"},
		{"subcommand kept", map[string]any{"command": "npm test"}, "npm test"},
		{"git subcommand", map[string]any{"command": `git commit -m "fix the thing"`}, "git commit"},
		{"flag is not a subcommand", map[string]any{"command": "ls -la /etc"}, "ls"},
		{
			// No shell metacharacter anywhere, so a "cut at the first
			// metacharacter" rule would have shipped the whole bearer
			// token.
			"secret in a header",
			map[string]any{"command": `curl -H "Authorization: Bearer sk-live-SECRET" https://api.example.com`},
			"curl",
		},
		// Digits never pass as a subcommand, so a numeric argument is
		// dropped too — and the metacharacter still ends the head.
		{"metacharacter terminates", map[string]any{"command": "sleep 12; touch probe.txt"}, "sleep"},
		{"hyphenated subcommand kept", map[string]any{"command": "git cherry-pick abc"}, "git cherry-pick"},
		{"docker subcommand kept", map[string]any{"command": "docker compose up"}, "docker compose"},

		// --- the command word itself (the first token was never checked) ---
		{"inline env secret before the command", map[string]any{"command": "GITHUB_TOKEN=ghp_abc123 gh api user"}, "gh api"},
		{"several assignments", map[string]any{"command": "A=1 B=two npm test"}, "npm test"},
		{"assignments and nothing else", map[string]any{"command": "SECRET=hunter2"}, ""},
		{"path command basenamed", map[string]any{"command": "/home/alice/bin/tool build"}, "tool build"},
		{"relative script basenamed", map[string]any{"command": "./scripts/deploy.sh"}, "deploy.sh"},
		{"windows command basenamed", map[string]any{"command": `C:\Users\alice\tool.exe build`}, "tool.exe build"},
		// The command word may carry digits; a filename argument is data.
		{"executable with digits kept, filename dropped", map[string]any{"command": "python3 manage.py"}, "python3"},
		{"sensitive filename argument dropped", map[string]any{"command": "terraform prod.tfvars"}, "terraform"},
		{"unexpanded variable as command", map[string]any{"command": "$DEPLOY_CMD --prod"}, ""},
		{"quoted command", map[string]any{"command": `"my tool" run`}, ""},
		{"not an assignment: equals mid-word", map[string]any{"command": "1FOO=bar run"}, ""},

		// --- credential- and host-shaped second words (the heuristic) ---
		{"key prefix with digits", map[string]any{"command": "mytool sk-live-abc123"}, "mytool"},
		{"key prefix, letters only", map[string]any{"command": "mytool sk-live-abcdef"}, "mytool"},
		{"slack-style token", map[string]any{"command": "notify xoxb-AAAA-BBBB"}, "notify"},
		{"github token", map[string]any{"command": "gh ghp_AbCdEf1234"}, "gh"},
		{"user at host", map[string]any{"command": "ssh deploy@prod-db"}, "ssh"},
		{"host and port", map[string]any{"command": "nc db.internal:5432"}, "nc"},
		{"user and secret", map[string]any{"command": "login admin:hunter"}, "login"},
		{"long opaque token", map[string]any{"command": "mytool AbCdEfGhIjKlMnOpQrStUv"}, "mytool"},
		{"pipe terminates", map[string]any{"command": "cat /etc/passwd | grep root"}, "cat"},
		{"env assignment refused", map[string]any{"command": "env TOKEN=secret deploy"}, "env"},
		{"path arg refused", map[string]any{"command": "python /home/dev/secret_script.py"}, "python"},
		{"empty command", map[string]any{"command": "   "}, ""},

		// --- urls ---
		{"url host only", map[string]any{"url": "https://api.example.com/v1/x?token=SECRET"}, "api.example.com"},
		{"url with port", map[string]any{"url": "http://localhost:8080/debug"}, "localhost:8080"},
		{"relative url has no host", map[string]any{"url": "/just/a/path"}, ""},

		// --- refusals ---
		{"unknown key yields nothing", map[string]any{"pattern": "password|secret"}, ""},
		{"empty input", map[string]any{}, ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := deriveToolTarget(tc.input); got != tc.want {
				t.Errorf("deriveToolTarget(%v) = %q, want %q", tc.input, got, tc.want)
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
