package session

// Conformance corpus for the VT snapshot path.
//
// Each fixture lives under internal/session/testdata/conformance/<name>/ with:
//
//   - input.bin        raw PTY bytes to feed the emulator
//   - meta.json        {"cols": int, "rows": int, "description": string}
//   - golden.snapshot  byte-exact expected output of NewVT(cols,rows).RenderSnapshot()
//                      after Write(input.bin)
//
// TestConformance replays each fixture and asserts the snapshot is byte-equal
// to the golden. This pins the snapshot contract against the current backend
// (hinshun/vt10x today) and becomes the migration safety net when an in-house
// emulator lands behind the same `*VT` surface.
//
// Maintenance:
//
//   - HIVE_VT_GEN_FIXTURES=1 go test ./internal/session/ -run TestSyntheticFixtures
//     (re)writes the canonical synthetic input.bin + meta.json files. Run only
//     when a fixture's input itself changes.
//
//   - HIVE_VT_UPDATE_GOLDEN=1 go test ./internal/session/ -run TestConformance
//     regenerates every golden.snapshot from the current backend's output. Run
//     after an intentional snapshot-format change, then review the diff.
//
// Adding new fixtures: drop a directory under testdata/conformance/ with
// input.bin + meta.json (capture via scripts/vtcapture or hand-craft via the
// synthetic generator), then run with HIVE_VT_UPDATE_GOLDEN=1 once.

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const conformanceDir = "testdata/conformance"

type fixtureMeta struct {
	Cols        int    `json:"cols"`
	Rows        int    `json:"rows"`
	Description string `json:"description,omitempty"`
	// Chunks splits input.bin into multiple Write calls. Each entry is the
	// byte length of one chunk; the entries must sum to len(input.bin).
	// Empty/missing means a single Write of the whole file. Real PTY data
	// arrives chunked, and the snapshot path's behavior depends on chunk
	// boundaries (scrollback eviction heuristic, UTF-8 split decode, …),
	// so fixtures that exercise those contracts must declare chunks here.
	Chunks []int `json:"chunks,omitempty"`
}

func TestConformance(t *testing.T) {
	entries, err := os.ReadDir(conformanceDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			t.Skip("no conformance fixtures present")
		}
		t.Fatalf("read conformance dir: %v", err)
	}
	update := os.Getenv("HIVE_VT_UPDATE_GOLDEN") == "1"

	any := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		any = true
		name := e.Name()
		t.Run(name, func(t *testing.T) {
			runConformanceFixture(t, name, update)
		})
	}
	if !any {
		t.Skip("conformance directory is empty")
	}
}

func runConformanceFixture(t *testing.T, name string, update bool) {
	t.Helper()
	dir := filepath.Join(conformanceDir, name)

	metaBytes, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var meta fixtureMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}
	if meta.Cols <= 0 || meta.Rows <= 0 {
		t.Fatalf("meta.json: cols/rows must be positive, got cols=%d rows=%d", meta.Cols, meta.Rows)
	}

	input, err := os.ReadFile(filepath.Join(dir, "input.bin"))
	if err != nil {
		t.Fatalf("read input.bin: %v", err)
	}

	v := NewVT(meta.Cols, meta.Rows)
	chunks, err := splitInputChunks(input, meta.Chunks)
	if err != nil {
		t.Fatalf("split input chunks: %v", err)
	}
	for i, c := range chunks {
		if _, err := v.Write(c); err != nil {
			t.Fatalf("VT.Write chunk %d: %v", i, err)
		}
	}
	got := v.RenderSnapshot()

	goldenPath := filepath.Join(dir, "golden.snapshot")
	if update {
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("updated %s (%d bytes)", goldenPath, len(got))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden.snapshot (run with HIVE_VT_UPDATE_GOLDEN=1 to seed): %v", err)
	}
	if string(got) != string(want) {
		t.Errorf("snapshot mismatch for %s\n  description: %s\n  cols=%d rows=%d input=%d bytes\n  want %d bytes: %s\n  got  %d bytes: %s",
			name, meta.Description, meta.Cols, meta.Rows, len(input),
			len(want), escapeForLog(want),
			len(got), escapeForLog(got))
	}
}

// splitInputChunks divides input into Write-sized chunks per the meta.Chunks
// directive. An empty/nil sizes slice means a single write of the whole input.
func splitInputChunks(input []byte, sizes []int) ([][]byte, error) {
	if len(sizes) == 0 {
		return [][]byte{input}, nil
	}
	total := 0
	for _, n := range sizes {
		if n <= 0 {
			return nil, fmt.Errorf("chunk size must be positive, got %d", n)
		}
		total += n
	}
	if total != len(input) {
		return nil, fmt.Errorf("chunk sizes sum to %d but input is %d bytes", total, len(input))
	}
	out := make([][]byte, 0, len(sizes))
	off := 0
	for _, n := range sizes {
		out = append(out, input[off:off+n])
		off += n
	}
	return out, nil
}

// escapeForLog returns a printable, single-line representation of b suitable
// for test failure output. Control bytes become \xNN; visible ASCII is kept.
// Output is capped so failures stay readable in CI logs.
func escapeForLog(b []byte) string {
	const cap = 240
	var sb strings.Builder
	sb.Grow(len(b))
	for i, c := range b {
		if i >= cap {
			fmt.Fprintf(&sb, "…(%d more bytes)", len(b)-cap)
			break
		}
		switch {
		case c == '\\':
			sb.WriteString(`\\`)
		case c == '"':
			sb.WriteString(`\"`)
		case c >= 0x20 && c < 0x7f:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, `\x%02x`, c)
		}
	}
	return `"` + sb.String() + `"`
}
