package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Pi's encoding is NOT claude's. Claude folds "." to "-" as well, so
// reusing encodeClaudeProjectDir here resolves to a directory that does
// not exist — and the failure is silent: the session just looks like it
// has no history. The dotted-worktree case is the one that catches it.
func TestEncodePiSessionsDir(t *testing.T) {
	cases := []struct {
		name string
		cwd  string
		want string
	}{
		{"plain", "/Users/u/repo", "--Users-u-repo--"},
		{"dotted worktree keeps its dot", "/Users/u/repo/.worktrees/x", "--Users-u-repo-.worktrees-x--"},
		{"trailing slash", "/Users/u/repo/", "--Users-u-repo--"},
		{"dotfile component", "/Users/u/.config/thing", "--Users-u-.config-thing--"},
		{"nested", "/a/b/c/d", "--a-b-c-d--"},
		{"colon", "/a/b:c", "--a-b-c--"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := encodePiSessionsDir(tc.cwd); got != tc.want {
				t.Fatalf("encodePiSessionsDir(%q) = %q, want %q", tc.cwd, got, tc.want)
			}
		})
	}
}

// Guards against the copy-paste that would break pi resolution.
func TestPiEncodingDiffersFromClaude(t *testing.T) {
	const cwd = "/Users/u/repo/.worktrees/x"
	if encodePiSessionsDir(cwd) == encodeClaudeProjectDir(cwd) {
		t.Fatal("pi and claude encodings must not coincide on a dotted path")
	}
}

// withHome points os.UserHomeDir at a temp dir for the duration of a
// test, and returns the pi sessions dir for cwd inside it.
func withHome(t *testing.T, cwd string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".pi", "agent", "sessions", encodePiSessionsDir(cwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func touch(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// pi echoes the --session-id it was handed straight into the filename,
// so the suffix is an exact handle. Sibling sessions in the same cwd
// must not be picked up.
func TestPiTranscriptPathsMatchesSessionIDSuffix(t *testing.T) {
	const cwd = "/Users/u/repo"
	dir := withHome(t, cwd)
	want := touch(t, dir, "2026-09-05T23-51-08-902Z_42c83acf-073d-4a50-a07d-3ecd7d80b73d.jsonl")
	touch(t, dir, "2026-09-05T23-52-08-902Z_deadbeef-0000-4000-8000-000000000000.jsonl")
	touch(t, dir, "2026-09-05T23-53-08-902Z_other.jsonl")

	got := piTranscriptPaths("42c83acf-073d-4a50-a07d-3ecd7d80b73d", cwd)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

// The session id is not always a uuid: Hive's own pi probe passes a
// literal string, which is what proved pi echoes the flag verbatim.
func TestPiTranscriptPathsAcceptsNonUUIDSessionID(t *testing.T) {
	const cwd = "/Users/u/repo"
	dir := withHome(t, cwd)
	want := touch(t, dir, "2026-09-07T01-43-35-209Z_hive-probe-1788745415.jsonl")

	got := piTranscriptPaths("hive-probe-1788745415", cwd)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

// Nothing guarantees pi writes one file per id; filename order is
// timestamp order, so sorting concatenates them correctly.
func TestPiTranscriptPathsSortsByTimestamp(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "42c83acf-073d-4a50-a07d-3ecd7d80b73d"
	dir := withHome(t, cwd)
	later := touch(t, dir, "2026-09-05T23-59-00-000Z_"+id+".jsonl")
	earlier := touch(t, dir, "2026-09-05T08-00-00-000Z_"+id+".jsonl")

	got := piTranscriptPaths(id, cwd)
	if len(got) != 2 || got[0] != earlier || got[1] != later {
		t.Fatalf("got %v, want [%s %s]", got, earlier, later)
	}
}

func TestPiTranscriptPathsEmptyWhenAbsent(t *testing.T) {
	const cwd = "/Users/u/repo"
	withHome(t, cwd)
	if got := piTranscriptPaths("nope", cwd); got != nil {
		t.Fatalf("got %v, want nil", got)
	}
}

func TestPiTranscriptPathsRejectsEmptyInputs(t *testing.T) {
	if got := piTranscriptPaths("", "/Users/u/repo"); got != nil {
		t.Fatalf("empty id: %v", got)
	}
	if got := piTranscriptPaths("id", ""); got != nil {
		t.Fatalf("empty cwd: %v", got)
	}
}

// A session id carrying a glob metacharacter would otherwise match
// another session's file.
func TestPiTranscriptPathsRejectsGlobMetacharacters(t *testing.T) {
	const cwd = "/Users/u/repo"
	dir := withHome(t, cwd)
	touch(t, dir, "2026-09-05T23-51-08-902Z_realsession.jsonl")
	if got := piTranscriptPaths("*", cwd); got != nil {
		t.Fatalf("glob id matched %v", got)
	}
}

// claudeDir points HOME at a temp dir and returns the Claude project
// directory for cwd inside it.
func claudeDir(t *testing.T, cwd string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home) // os.UserHomeDir on Windows
	dir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

// claudeFile writes a transcript whose records carry the given ids, the
// shape verified on a real fork: camelCase sessionId is the file's own,
// snake_case session_id is the conversation it descends from. mod sets
// the file's mtime so ordering is explicit rather than timing-dependent.
func claudeFile(t *testing.T, dir, own, origin string, mod time.Time) string {
	t.Helper()
	p := filepath.Join(dir, own+".jsonl")
	body := `{"type":"file-history-snapshot","messageId":"m1"}` + "\n" +
		`{"type":"assistant","sessionId":"` + own + `","session_id":"` + origin + `","message":{"role":"assistant","content":[{"type":"text","text":"hi"}]}}` + "\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, mod, mod); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClaudeTranscriptPaths(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "be0af6d6-947e-468c-a7e2-518a869cd69b"
	dir := claudeDir(t, cwd)

	// Absent until claude writes it: a session started but never used
	// has no transcript, and that must not read as an error.
	if got := claudeTranscriptPaths(id, cwd); got != nil {
		t.Fatalf("expected nil before the file exists, got %v", got)
	}

	want := claudeFile(t, dir, id, id, time.Now())
	got := claudeTranscriptPaths(id, cwd)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
	}
}

// The bug this exists for: Claude forked the conversation into a new file
// (a new sessionId, the full history copied, session_id pointing back),
// the original stopped growing, and the transcript was cut off at the
// fork because only <id>.jsonl was read.
func TestClaudeTranscriptPathsFollowsFork(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "bad19ab3-d4b8-4d22-9368-ebc95f073b92"
	dir := claudeDir(t, cwd)
	now := time.Now()
	claudeFile(t, dir, id, id, now.Add(-time.Hour))
	fork := claudeFile(t, dir, "c49564f2-afdb-403c-9efe-a52b5fa126db", id, now)

	got := claudeTranscriptPaths(id, cwd)
	if len(got) != 1 || got[0] != fork {
		t.Fatalf("got %v, want the fork [%s]", got, fork)
	}
}

// The resolution is memoized on the directory mtime; a fork created
// AFTER a first lookup must still be picked up on the next one.
func TestClaudeTranscriptPathsNoticesLaterFork(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "orig"
	dir := claudeDir(t, cwd)
	now := time.Now()
	own := claudeFile(t, dir, id, id, now.Add(-time.Hour))
	if got := claudeTranscriptPaths(id, cwd); len(got) != 1 || got[0] != own {
		t.Fatalf("before the fork: got %v", got)
	}
	fork := claudeFile(t, dir, "fork", id, now)
	// Make the directory change unambiguous even on a coarse clock.
	later := now.Add(time.Minute)
	if err := os.Chtimes(dir, later, later); err != nil {
		t.Fatal(err)
	}
	if got := claudeTranscriptPaths(id, cwd); len(got) != 1 || got[0] != fork {
		t.Fatalf("after the fork: got %v, want [%s]", got, fork)
	}
}

// A fork of a fork: the newest descendant wins.
func TestClaudeTranscriptPathsPicksNewestFork(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "orig"
	dir := claudeDir(t, cwd)
	now := time.Now()
	claudeFile(t, dir, id, id, now.Add(-2*time.Hour))
	claudeFile(t, dir, "fork-a", id, now.Add(-time.Hour))
	newest := claudeFile(t, dir, "fork-b", id, now)

	got := claudeTranscriptPaths(id, cwd)
	if len(got) != 1 || got[0] != newest {
		t.Fatalf("got %v, want [%s]", got, newest)
	}
}

// Sibling sessions share the project directory, and they are usually the
// newer files. Only a file that descends from THIS session may win.
func TestClaudeTranscriptPathsIgnoresNewerUnrelatedSessions(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "mine"
	dir := claudeDir(t, cwd)
	now := time.Now()
	own := claudeFile(t, dir, id, id, now.Add(-time.Hour))
	claudeFile(t, dir, "sibling", "sibling", now)

	got := claudeTranscriptPaths(id, cwd)
	if len(got) != 1 || got[0] != own {
		t.Fatalf("got %v, want own [%s]", got, own)
	}
}

// A fork already contains the history, so it replaces the original
// rather than being appended to it — never both.
func TestClaudeTranscriptPathsNeverConcatenates(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "orig"
	dir := claudeDir(t, cwd)
	now := time.Now()
	claudeFile(t, dir, id, id, now.Add(-time.Hour))
	claudeFile(t, dir, "fork", id, now)
	if got := claudeTranscriptPaths(id, cwd); len(got) != 1 {
		t.Fatalf("got %d paths, want exactly 1: %v", len(got), got)
	}
}

// Only files written after the original are candidates: a fork is newer
// than the file it copied. An OLDER file claiming the id is not read.
func TestClaudeTranscriptPathsSkipsOlderFiles(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "orig"
	dir := claudeDir(t, cwd)
	now := time.Now()
	own := claudeFile(t, dir, id, id, now)
	claudeFile(t, dir, "older", id, now.Add(-time.Hour))
	got := claudeTranscriptPaths(id, cwd)
	if len(got) != 1 || got[0] != own {
		t.Fatalf("got %v, want own [%s]", got, own)
	}
}

// The origin sits past large leading records in a real file; the scan
// must reach it.
func TestClaudeForkOriginSkipsLeadingRecords(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.jsonl")
	var b strings.Builder
	for range 200 {
		b.WriteString(`{"type":"attachment","attachment":{"blob":"` + strings.Repeat("x", 1000) + `"}}` + "\n")
	}
	b.WriteString(`{"type":"assistant","session_id":"origin-id"}` + "\n")
	if err := os.WriteFile(p, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := claudeForkOrigin(p); got != "origin-id" {
		t.Fatalf("got %q", got)
	}
}

// The catalog is what the daemon asks; a nil TranscriptPaths is how an
// agent says "I keep no transcript Hive can read".
func TestTranscriptPathsWiredForClaudeAndPiOnly(t *testing.T) {
	withTranscripts := map[ID]bool{IDClaude: true, IDPi: true}
	for id, def := range defsByID {
		has := def.TranscriptPaths != nil
		if has != withTranscripts[id] {
			t.Errorf("agent %s: TranscriptPaths set=%v, want %v", id, has, withTranscripts[id])
		}
	}
}
