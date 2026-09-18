package agent

import (
	"os"
	"path/filepath"
	"testing"
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

func TestClaudeTranscriptPaths(t *testing.T) {
	const cwd = "/Users/u/repo"
	const id = "be0af6d6-947e-468c-a7e2-518a869cd69b"
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "projects", encodeClaudeProjectDir(cwd))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}

	// Absent until claude writes it: a session started but never used
	// has no transcript, and that must not read as an error.
	if got := claudeTranscriptPaths(id, cwd); got != nil {
		t.Fatalf("expected nil before the file exists, got %v", got)
	}

	want := filepath.Join(dir, id+".jsonl")
	if err := os.WriteFile(want, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := claudeTranscriptPaths(id, cwd)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("got %v, want [%s]", got, want)
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
