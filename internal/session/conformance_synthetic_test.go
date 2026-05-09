package session

// Synthetic fixture generator for the conformance corpus.
//
// Real-app captures (from scripts/vtcapture) are the long-term goal, but we
// also need small, hand-crafted fixtures that pin specific contracts (RGB,
// reverse video, alt-screen, scrollback eviction, OSC 8, …). Those live here
// as Go literals so they're reviewable, then get materialized to disk on
// demand.
//
// Usage:
//
//   HIVE_VT_GEN_FIXTURES=1 go test ./internal/session/ -run TestSyntheticFixtures
//
// This rewrites testdata/conformance/<name>/{input.bin,meta.json} for every
// synthetic fixture below, leaving golden.snapshot alone. After regenerating,
// run with HIVE_VT_UPDATE_GOLDEN=1 to refresh goldens.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type syntheticFixture struct {
	name        string
	cols, rows  int
	description string
	input       string
	// chunks declares how input should be split into Write calls. Each
	// entry is a byte length; entries must sum to len(input). Empty means
	// a single Write of the whole input.
	chunks []int
}

// syntheticFixtures lists every hand-crafted fixture in the corpus. Order
// doesn't matter; names are used as directory names under testdata/conformance.
//
// Each fixture should pin one specific contract from the snapshot path. Keep
// inputs small — large captures belong in real PTY recordings via scripts/vtcapture.
var syntheticFixtures = []syntheticFixture{
	{
		name:        "plain-text-bold",
		cols:        20,
		rows:        5,
		description: "ASCII text with a bold word; baseline round-trip and SGR",
		input:       "hello\r\n\x1b[1mworld\x1b[m",
	},
	{
		name:        "rgb-truecolor",
		cols:        20,
		rows:        2,
		description: "24-bit RGB foreground; guards against legacy backends dropping truecolor",
		input:       "\x1b[38;2;255;128;64morange\x1b[m\r\n\x1b[48;2;0;128;255mblue-bg\x1b[m",
	},
	{
		name:        "reverse-video-no-double-apply",
		cols:        10,
		rows:        1,
		description: "Reverse video on red-on-white must not double-apply across snapshot replay",
		input:       "\x1b[31;47;7mX\x1b[m",
	},
	{
		name:        "alt-screen",
		cols:        20,
		rows:        3,
		description: "Snapshot taken in alt-screen must enter alt-screen first so live ?1049l swaps cleanly",
		input:       "\x1b[?1049hALT-CONTENT",
	},
	{
		name:        "alt-screen-roundtrip-preserves-scrollback",
		cols:        20,
		rows:        3,
		description: "Vim-like enter/exit alt-screen must not erase prior normal-screen scrollback",
		// One chunk per "normal-line\r\n" so scrollback eviction fires per
		// line (the heuristic is per-Write), then alt-enter and alt-exit
		// each in their own write to mirror real PTY chunking.
		input: strings.Repeat("normal-line\r\n", 10) +
			"\x1b[?1049h" + "ALT" +
			"\x1b[?1049l",
		chunks: append(
			repeatN(len("normal-line\r\n"), 10),
			len("\x1b[?1049hALT"),
			len("\x1b[?1049l"),
		),
	},
	{
		name:        "scrollback-overflow",
		cols:        20,
		rows:        3,
		description: "More lines than the visible viewport, chunked per line so eviction fires; scrollback ring captures the overflow",
		input:       buildScrollbackInput(15),
		chunks:      repeatN(len("line-NN\r\n"), 15),
	},
	{
		name:        "scrollback-styled-line-survives-eviction",
		cols:        20,
		rows:        3,
		description: "A bold red line scrolled off the viewport must keep its SGR in the captured ring; chunked per line",
		input:       "\x1b[1;31mRED-BOLD\x1b[m\r\n" + strings.Repeat("plain\r\n", 10),
		chunks: append(
			[]int{len("\x1b[1;31mRED-BOLD\x1b[m\r\n")},
			repeatN(len("plain\r\n"), 10)...,
		),
	},
	{
		name:        "clear-screen-not-pushed-to-scrollback",
		cols:        20,
		rows:        3,
		description: "Content followed by ESC[2J in one Write must not leak the cleared content into scrollback",
		input:       "before-clear\r\n\x1b[H\x1b[2J",
	},
	{
		name:        "clear-screen-across-chunks",
		cols:        20,
		rows:        3,
		description: "Content and the clear sequence arrive in separate Writes; the post-blank-screen guard must reject the eviction match",
		input:       "line-A\r\nline-B\r\n\x1b[H\x1b[2J",
		chunks:      []int{len("line-A\r\nline-B\r\n"), len("\x1b[H\x1b[2J")},
	},
	{
		name:        "cjk-wide-chars",
		cols:        20,
		rows:        2,
		description: "CJK and emoji wide chars; column alignment must not desync from xterm.js",
		input:       "中文\r\n🎉🚀",
	},
	{
		name:        "osc-8-hyperlink",
		cols:        40,
		rows:        2,
		description: "OSC 8 hyperlink sequence; defines the strip-vs-preserve behavior we ship today",
		input:       "\x1b]8;;https://example.com/\x1b\\example link\x1b]8;;\x1b\\",
	},
}

// repeatN returns a slice of n copies of v. Used to express per-line chunk
// directives where every chunk has the same byte length.
func repeatN(v, n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = v
	}
	return out
}

// buildScrollbackInput emits n distinct CRLF-terminated lines suitable for
// triggering scrollback eviction in a small viewport.
func buildScrollbackInput(n int) string {
	var sb strings.Builder
	for i := range n {
		sb.WriteString("line-")
		sb.WriteByte(byte('0' + (i/10)%10))
		sb.WriteByte(byte('0' + i%10))
		sb.WriteString("\r\n")
	}
	return sb.String()
}

func TestSyntheticFixtures(t *testing.T) {
	if os.Getenv("HIVE_VT_GEN_FIXTURES") != "1" {
		t.Skip("set HIVE_VT_GEN_FIXTURES=1 to (re)write synthetic fixture inputs and meta")
	}
	for _, f := range syntheticFixtures {
		dir := filepath.Join(conformanceDir, f.name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		if err := os.WriteFile(filepath.Join(dir, "input.bin"), []byte(f.input), 0o644); err != nil {
			t.Fatalf("write input.bin for %s: %v", f.name, err)
		}
		meta := fixtureMeta{Cols: f.cols, Rows: f.rows, Description: f.description, Chunks: f.chunks}
		b, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			t.Fatalf("marshal meta for %s: %v", f.name, err)
		}
		b = append(b, '\n')
		if err := os.WriteFile(filepath.Join(dir, "meta.json"), b, 0o644); err != nil {
			t.Fatalf("write meta.json for %s: %v", f.name, err)
		}
		t.Logf("wrote synthetic fixture %s (input=%d bytes, %dx%d)", f.name, len(f.input), f.cols, f.rows)
	}
}
