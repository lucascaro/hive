package transcript

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func lines(t *testing.T, recs ...string) []Line {
	t.Helper()
	got, _, err := projectFile(strings.NewReader(strings.Join(recs, "")), nil)
	if err != nil {
		t.Fatalf("projectFile: %v", err)
	}
	return got
}

func TestProjectClaudeStringContent(t *testing.T) {
	got := lines(t, `{"type":"user","message":{"role":"user","content":"hello world"}}`+"\n")
	if len(got) != 1 || got[0].Text != "hello world" || got[0].Role != "user" {
		t.Fatalf("got %+v", got)
	}
}

func TestProjectClaudeBlockArrayContent(t *testing.T) {
	got := lines(t, `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"alpha"},{"type":"text","text":"beta"}]}}`+"\n")
	if len(got) != 2 || got[0].Text != "alpha" || got[1].Text != "beta" {
		t.Fatalf("got %+v", got)
	}
}

// The string-or-array ambiguity repeats inside a tool_result block, one
// level deeper than the message's own content.
func TestProjectClaudeNestedToolResultContent(t *testing.T) {
	strForm := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":"plain output"}]}}` + "\n"
	arrForm := `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":[{"type":"text","text":"block output"}]}]}}` + "\n"
	got := lines(t, strForm, arrForm)
	if len(got) != 2 || got[0].Text != "plain output" || got[1].Text != "block output" {
		t.Fatalf("got %+v", got)
	}
}

func TestProjectSkipsThinkingAndToolUse(t *testing.T) {
	got := lines(t, `{"type":"assistant","message":{"role":"assistant","content":[`+
		`{"type":"thinking","thinking":"secret reasoning"},`+
		`{"type":"tool_use","name":"Bash","input":{"command":"ls"}},`+
		`{"type":"text","text":"visible"}]}}`+"\n")
	if len(got) != 1 || got[0].Text != "visible" {
		t.Fatalf("expected only the text block, got %+v", got)
	}
}

func TestProjectSkipsNonMessageRecordTypes(t *testing.T) {
	got := lines(t,
		`{"type":"cost-state","totalCostUSD":1.5}`+"\n",
		`{"type":"mode","mode":"acceptEdits"}`+"\n",
		`{"type":"user","message":{"role":"user","content":"kept"}}`+"\n")
	if len(got) != 1 || got[0].Text != "kept" {
		t.Fatalf("got %+v", got)
	}
}

// One malformed line must not cost the user the rest of the file.
func TestProjectToleratesMalformedLine(t *testing.T) {
	got := lines(t,
		`{"type":"user","message":{"role":"user","content":"before"}}`+"\n",
		"{not json at all\n",
		`{"type":"user","message":{"role":"user","content":"after"}}`+"\n")
	if len(got) != 2 || got[0].Text != "before" || got[1].Text != "after" {
		t.Fatalf("got %+v", got)
	}
}

func TestProjectSplitsEmbeddedNewlines(t *testing.T) {
	got := lines(t, `{"type":"user","message":{"role":"user","content":"one\ntwo\nthree"}}`+"\n")
	if len(got) != 3 {
		t.Fatalf("want 3 lines, got %+v", got)
	}
	for i, want := range []string{"one", "two", "three"} {
		if got[i].Text != want || got[i].Index != i {
			t.Fatalf("line %d = %+v, want %q", i, got[i], want)
		}
	}
}

// Columns must be counted over the text the caller receives, not over
// the raw record: a Col measured across escape bytes highlights the
// wrong characters.
func TestProjectStripsControlCharacters(t *testing.T) {
	// Built with json.Marshal rather than a hand-written literal: the
	// fixture needs a real ESC inside a JSON string, and writing that
	// by hand produces either an invalid raw control byte or an escape
	// that is easy to get subtly wrong.
	esc := "a\x1b[31mred\x1b[0m\tkept"
	blob, err := json.Marshal(map[string]any{
		"type":    "user",
		"message": map[string]any{"role": "user", "content": esc},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := lines(t, string(blob)+"\n")
	if len(got) != 1 {
		t.Fatalf("got %+v", got)
	}
	if strings.ContainsRune(got[0].Text, 0x1b) {
		t.Fatalf("escape survived: %q", got[0].Text)
	}
	if !strings.Contains(got[0].Text, "\t") {
		t.Fatalf("tab should be kept: %q", got[0].Text)
	}
	m, _ := Search(got, "red", 10)
	if len(m) != 1 {
		t.Fatalf("want one match, got %+v", m)
	}
	if c := m[0].Col; got[0].Text[c:c+3] != "red" {
		t.Fatalf("Col %d does not index Text %q", c, got[0].Text)
	}
}

func TestSearchIsCaseInsensitiveSubstring(t *testing.T) {
	got := lines(t, `{"type":"user","message":{"role":"user","content":"Error and error"}}`+"\n")
	m, trunc := Search(got, "ERROR", 10)
	if trunc || len(m) != 2 {
		t.Fatalf("got %+v trunc=%v", m, trunc)
	}
	// Newest first: within a line the later column comes first.
	if m[0].Col != 10 || m[1].Col != 0 {
		t.Fatalf("columns %d,%d, want 10,0", m[0].Col, m[1].Col)
	}
	if m[0].Len != 5 {
		t.Fatalf("Len = %d, want the query length", m[0].Len)
	}
}

func TestSearchTruncatesAtLimit(t *testing.T) {
	got := lines(t, `{"type":"user","message":{"role":"user","content":"x x x x x"}}`+"\n")
	m, trunc := Search(got, "x", 3)
	if !trunc || len(m) != 3 {
		t.Fatalf("got %d matches trunc=%v", len(m), trunc)
	}
}

// Most recent first: the match nearest the bottom of the transcript is
// the first one the user sees.
func TestSearchIsNewestFirst(t *testing.T) {
	got := lines(t,
		rec("needle one"),
		rec("filler"),
		rec("needle two"),
		rec("needle three"))
	m, _ := Search(got, "needle", 10)
	if len(m) != 3 {
		t.Fatalf("got %+v", m)
	}
	for i, want := range []int{3, 2, 0} {
		if m[i].Line != want {
			t.Fatalf("match %d on line %d, want %d (newest first): %+v", i, m[i].Line, want, m)
		}
	}
}

// A capped search must keep the NEWEST matches. Scanning oldest-first
// and cutting at the limit would drop exactly the matches the user is
// looking for.
func TestSearchTruncationKeepsNewest(t *testing.T) {
	var recs []string
	for i := range 10 {
		recs = append(recs, rec(fmt.Sprintf("needle %d", i)))
	}
	got := lines(t, recs...)
	m, trunc := Search(got, "needle", 3)
	if !trunc || len(m) != 3 {
		t.Fatalf("got %d trunc=%v", len(m), trunc)
	}
	for i, want := range []int{9, 8, 7} {
		if m[i].Line != want {
			t.Fatalf("kept line %d at %d, want %d — truncation dropped the newest: %+v", m[i].Line, i, want, m)
		}
	}
}

func TestSearchEmptyQueryFindsNothing(t *testing.T) {
	got := lines(t, `{"type":"user","message":{"role":"user","content":"anything"}}`+"\n")
	if m, _ := Search(got, "", 10); len(m) != 0 {
		t.Fatalf("empty query matched %+v", m)
	}
}

func TestCapTextIsRuneSafe(t *testing.T) {
	s := strings.Repeat("é", 10) // 2 bytes each
	out, cut := CapText(s, 5)
	if !cut {
		t.Fatal("expected a cut")
	}
	if !utf8.ValidString(out) {
		t.Fatalf("split a rune: %q", out)
	}
	if len(out) > 5 {
		t.Fatalf("len %d over cap", len(out))
	}
}

func TestCapTextLeavesShortStrings(t *testing.T) {
	if out, cut := CapText("short", 100); cut || out != "short" {
		t.Fatalf("got %q cut=%v", out, cut)
	}
}

func TestWindowCentersAndClamps(t *testing.T) {
	var ls []Line
	for i := 0; i < 100; i++ {
		ls = append(ls, Line{Index: i})
	}
	// Centered in the middle.
	_, start := Window(ls, 50, 10)
	if start != 45 {
		t.Fatalf("center 50 count 10 -> start %d, want 45", start)
	}
	// Near the head: must not go negative.
	if _, s := Window(ls, 2, 40); s != 0 {
		t.Fatalf("head clamp -> %d, want 0", s)
	}
	// Near the tail: must not run past the end.
	if got, s := Window(ls, 99, 40); s != 60 || len(got) != 40 {
		t.Fatalf("tail clamp -> start %d len %d, want 60/40", s, len(got))
	}
	// Count larger than the list.
	if got, s := Window(ls, 0, 500); s != 0 || len(got) != 100 {
		t.Fatalf("oversize -> start %d len %d", s, len(got))
	}
}

// --- cache ---

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func rec(text string) string {
	return `{"type":"user","message":{"role":"user","content":"` + text + `"}}` + "\n"
}

func TestCacheReusesProjectionWhenFileUnchanged(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.jsonl", rec("alpha"))
	var c Cache
	first, err := c.Lines("s1", []string{p})
	if err != nil {
		t.Fatal(err)
	}
	// Make the file unreadable; a cache that re-read would fail.
	if err := os.Chmod(p, 0o000); err != nil {
		t.Skip("cannot chmod in this environment")
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })
	second, err := c.Lines("s1", []string{p})
	if err != nil {
		t.Fatalf("unchanged file should not be re-read: %v", err)
	}
	if len(first) != len(second) || len(second) != 1 {
		t.Fatalf("got %d then %d", len(first), len(second))
	}
}

func TestCacheReparsesOnlyAppendedTail(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.jsonl", rec("alpha"))
	var c Cache
	if _, err := c.Lines("s1", []string{p}); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(rec("beta"))
	f.Close()

	got, err := c.Lines("s1", []string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "alpha" || got[1].Text != "beta" {
		t.Fatalf("got %+v", got)
	}
	if got[1].Index != 1 {
		t.Fatalf("appended line index = %d, want 1", got[1].Index)
	}
}

// The one that fails on the naive implementation. A transcript observed
// mid-write has a half-flushed final record; consuming those bytes
// while skipping the unparseable line drops it permanently, so the most
// recent line — the one the user is most likely looking for — never
// becomes searchable.
func TestCacheHoldsBackPartialFinalLine(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.jsonl", rec("alpha"))
	var c Cache
	if _, err := c.Lines("s1", []string{p}); err != nil {
		t.Fatal(err)
	}

	full := rec("betaline")
	half := full[:len(full)/2] // no trailing newline
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(half)
	f.Close()

	got, err := c.Lines("s1", []string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("partial record must not project: %+v", got)
	}

	// Complete the record. It must now become visible — which it cannot
	// if its opening bytes were consumed by the previous refresh.
	f, _ = os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o600)
	f.WriteString(full[len(half):])
	f.Close()

	got, err = c.Lines("s1", []string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[1].Text != "betaline" {
		t.Fatalf("completed record never became searchable: %+v", got)
	}
}

func TestCacheDropsOnSessionChange(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.jsonl", rec("alpha"))
	b := writeFile(t, dir, "b.jsonl", rec("bravo")+rec("charlie"))
	var c Cache
	if _, err := c.Lines("s1", []string{a}); err != nil {
		t.Fatal(err)
	}
	got, err := c.Lines("s2", []string{b})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "bravo" {
		t.Fatalf("switching sessions must not retain the old projection: %+v", got)
	}
}

// A rewritten or rotated file invalidates every stored offset; seeking
// past its new EOF would silently return nothing.
func TestCacheFullReparseWhenFileShrinks(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.jsonl", rec("alpha")+rec("beta")+rec("gamma"))
	var c Cache
	if got, _ := c.Lines("s1", []string{p}); len(got) != 3 {
		t.Fatalf("setup: %d lines", len(got))
	}
	writeFile(t, dir, "a.jsonl", rec("fresh"))
	got, err := c.Lines("s1", []string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Text != "fresh" {
		t.Fatalf("got %+v", got)
	}
}

func TestCacheDropReleases(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "a.jsonl", rec("alpha"))
	var c Cache
	if _, err := c.Lines("s1", []string{p}); err != nil {
		t.Fatal(err)
	}
	c.Drop()
	if c.lines != nil || c.key != "" {
		t.Fatalf("Drop left state: key=%q lines=%d", c.key, len(c.lines))
	}
}

func TestReadAllConcatenatesInOrder(t *testing.T) {
	dir := t.TempDir()
	a := writeFile(t, dir, "a.jsonl", rec("first"))
	b := writeFile(t, dir, "b.jsonl", rec("second"))
	got, err := ReadAll([]string{a, b})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Text != "first" || got[1].Text != "second" {
		t.Fatalf("got %+v", got)
	}
}
