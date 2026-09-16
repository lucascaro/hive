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
