package session

import (
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

// OSC 7501 (spec 517): the scanner reads Claude's status reports out of
// the PTY stream, and the session answers the capability query that
// makes Claude emit them.

func scanAll(chunks ...string) (rang, query bool, statuses []Status) {
	var s bellScanner
	for _, c := range chunks {
		if s.Scan([]byte(c)) {
			rang = true
		}
	}
	query, statuses = s.Take()
	return
}

// wantClaudeStream is what testdata/osc7501/claude-2.1.300.bin holds: a
// recorded Claude 2.1.300 run (startup, a tool turn, a question, Esc,
// exit), reduced to its OSC 7501 sequences with their real terminators.
var wantClaudeStream = []Status{
	{State: "idle"},
	{State: "working"},
	{State: "working"},
	{State: "done"},
	{State: "working"},
	{State: "blocked", Kind: "question"},
	{State: "idle"},
	{State: "clear"}, {State: "clear"}, {State: "clear"},
}

func TestStatusScannerParsesRecordedClaudeStream(t *testing.T) {
	raw, err := os.ReadFile("testdata/osc7501/claude-2.1.300.bin")
	if err != nil {
		t.Fatal(err)
	}
	rang, query, got := scanAll(string(raw))
	if rang {
		t.Error("an OSC terminator was read as a bell")
	}
	if !query {
		t.Error("the capability query was not seen")
	}
	if !reflect.DeepEqual(got, wantClaudeStream) {
		t.Errorf("statuses = %+v, want %+v", got, wantClaudeStream)
	}
}

// A PTY read splits a sequence anywhere; the stream must parse the same
// from every cut.
// The Go probe's recording (TestClaudeProbeOSC7501EscGoesIdle, Claude
// 2.1.301): a permission dialog (blocked/permission) dismissed with Esc,
// Esc mid-turn, and Esc during a running tool. Esc is `idle` each time.
func TestStatusScannerParsesRecordedProbeStream(t *testing.T) {
	raw, err := os.ReadFile("testdata/osc7501/claude-2.1.301-probe.bin")
	if err != nil {
		t.Fatal(err)
	}
	_, query, got := scanAll(string(raw))
	want := []Status{
		{State: "idle"}, {State: "working"},
		{State: "blocked", Kind: "permission"}, {State: "idle"}, // dialog, then Esc
		{State: "working"}, {State: "idle"}, // essay, then Esc
		{State: "working"}, {State: "idle"}, // tool, then Esc
	}
	if !query || !reflect.DeepEqual(got, want) {
		t.Errorf("query=%v statuses = %+v, want %+v", query, got, want)
	}
}

func TestStatusScannerSplitAtEveryOffset(t *testing.T) {
	raw, err := os.ReadFile("testdata/osc7501/claude-2.1.300.bin")
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < len(raw); i++ {
		_, query, got := scanAll(string(raw[:i]), string(raw[i:]))
		if !query || !reflect.DeepEqual(got, wantClaudeStream) {
			t.Fatalf("split at %d: query=%v statuses=%+v", i, query, got)
		}
	}
}

// Review round 1: a question's base64 msg easily outruns the payload
// prefix, and it must not cost the report.
func TestStatusScannerLongMsgStillParsesStateAndKind(t *testing.T) {
	long := "\x1b]7501;state=blocked:app=claude-code:kind=question:msg=" + strings.Repeat("QUJD", 2000) + "\x1b\\"
	_, _, got := scanAll(long)
	want := []Status{{State: "blocked", Kind: "question"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statuses = %+v, want %+v", got, want)
	}
}

func TestStatusScannerBELAndSTTerminators(t *testing.T) {
	_, _, got := scanAll("\x1b]7501;state=working\x07", "\x1b]7501;state=idle\x1b\\")
	want := []Status{{State: "working"}, {State: "idle"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("statuses = %+v, want %+v", got, want)
	}
}

func TestStatusScannerIgnoresOtherOSCAndStaysInSyncWithBells(t *testing.T) {
	rang, query, got := scanAll("\x1b]2;~/src/hive\x07", "\x1b]7502;state=working\x07", "\x1b]7501;nonsense\x07", "ding\x07")
	if !rang || query || len(got) != 0 {
		t.Errorf("rang=%v query=%v statuses=%+v, want only the real bell", rang, query, got)
	}
}

// An unterminated sequence never classifies, and ends at the next
// terminator without desyncing bell detection.
func TestStatusScannerUnterminatedOSCEndsAtNextTerminator(t *testing.T) {
	rang, _, got := scanAll("\x1b]7501;state=working"+strings.Repeat("x", 4000), "\x07", "\x07")
	if !rang {
		t.Error("the bell after the finished OSC was swallowed")
	}
	if len(got) != 0 {
		t.Errorf("a payload cut off mid-value produced a report: %+v", got)
	}
}

func TestStatusScannerCapsPendingReports(t *testing.T) {
	_, _, got := scanAll(strings.Repeat("\x1b]7501;state=working\x07", 100))
	if len(got) != maxPendingStatuses {
		t.Errorf("pending = %d, want the cap %d", len(got), maxPendingStatuses)
	}
}

// echoReply runs a bash script in a PTY session. The script prints the
// 7501 query, then waits up to a second for a reply on stdin and prints
// what it read, bracketed.
const askAndEcho = `stty raw -echo; printf '\033]7501;?\007'; ` +
	`IFS= read -r -s -n 6 -t 2 x; printf 'REPLY[%s]' "$(printf '%s' "$x" | od -An -c | tr -d ' \n')"`

func runAsk(t *testing.T, answer bool) string {
	t.Helper()
	skipOnWindows(t)
	sess, err := Start(Options{Shell: "/bin/bash", Cmd: []string{"/bin/bash", "-c", askAndEcho}, Cols: 80, Rows: 24, AnswerStatusQuery: answer})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer sess.Close()
	sink := &bufSinkMu{}
	unsub, err := sess.SubscribeWithAtomicReplay(sink, func(replay []byte) error { _, _ = sink.Write(replay); return nil })
	if err != nil {
		t.Fatal(err)
	}
	defer unsub()
	if !drainUntil(func() bool { return strings.Contains(sink.String(), "REPLY[") }, 6*time.Second) {
		t.Fatalf("script never finished: %q", sink.String())
	}
	return sink.String()
}

func TestQueryAnsweredForAgentSessions(t *testing.T) {
	out := runAsk(t, true)
	// od -c spells the first six bytes of ESC ] 7 5 0 1 ; ? ESC \.
	if !strings.Contains(out, "REPLY[033]7501]") {
		t.Errorf("agent session got no reply: %q", out)
	}
}

func TestQueryNotAnsweredForShellSessions(t *testing.T) {
	out := runAsk(t, false)
	if !strings.Contains(out, "REPLY[]") {
		t.Errorf("a plain shell session was sent a reply: %q", out)
	}
}

// The reply is once per spawn: a second query (a program re-asking, or
// output that merely contains one) writes nothing more.
func TestQueryAnsweredOncePerSpawn(t *testing.T) {
	skipOnWindows(t)
	script := `stty raw -echo min 0 time 10; printf '\033]7501;?\007'; sleep 0.3; printf '\033]7501;?\007'; sleep 0.3; ` +
		`x=$(dd bs=100 count=1 2>/dev/null | od -An -c | tr -d ' \n'); printf 'GOT[%s]' "$x"`
	sess, err := Start(Options{Shell: "/bin/bash", Cmd: []string{"/bin/bash", "-c", script}, Cols: 80, Rows: 24, AnswerStatusQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	sink := &bufSinkMu{}
	unsub, _ := sess.SubscribeWithAtomicReplay(sink, func(replay []byte) error { _, _ = sink.Write(replay); return nil })
	defer unsub()
	if !drainUntil(func() bool { return strings.Contains(sink.String(), "GOT[") }, 8*time.Second) {
		t.Fatalf("script never finished: %q", sink.String())
	}
	out := sink.String()
	i := strings.Index(out, "GOT[")
	got := out[i:]
	// One reply is ten bytes, ESC ] 7 5 0 1 ; ? ESC \; od -c spells
	// each ESC "033", so "033" appears twice per reply.
	if n := strings.Count(got, "033"); n != 2 {
		t.Errorf("want exactly one reply (two ESC), got %d ESC in %q", n, got)
	}
}

// The reply must never stall the PTY drain. A child that is not reading
// stdin while its input queue is full would otherwise deadlock readLoop
// against its own stdout.
func TestQueryAnswerNeverBlocksReadLoop(t *testing.T) {
	skipOnWindows(t)
	script := `stty raw -echo; sleep 0.5; printf '\033]7501;?\007'; sleep 0.2; printf '\033]7501;state=done\007'; sleep 5`
	sess, err := Start(Options{Shell: "/bin/bash", Cmd: []string{"/bin/bash", "-c", script}, Cols: 80, Rows: 24, AnswerStatusQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var mu sync.Mutex
	var got []Status
	sess.SetStatusHook(func(st Status) { mu.Lock(); got = append(got, st); mu.Unlock() })
	// Fill the child's stdin queue: it never reads, so this blocks.
	go func() { _, _ = sess.Write([]byte(strings.Repeat("x", 1<<20))) }()
	if !drainUntil(func() bool { mu.Lock(); defer mu.Unlock(); return len(got) > 0 }, 6*time.Second) {
		t.Fatal("status after the query never arrived: the reply blocked the read loop")
	}
}

// Replay reads the VT snapshot and never the scanner, so a ring that
// holds a query cannot re-trigger anything. This is a structural guard:
// attach many times, and nothing is delivered again.
func TestReplayIsStructurallyUnscanned(t *testing.T) {
	skipOnWindows(t)
	script := `printf '\033]7501;?\007\033]7501;state=working\007'; sleep 5`
	sess, err := Start(Options{Shell: "/bin/bash", Cmd: []string{"/bin/bash", "-c", script}, Cols: 80, Rows: 24, AnswerStatusQuery: true})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var mu sync.Mutex
	n := 0
	sess.SetStatusHook(func(Status) { mu.Lock(); n++; mu.Unlock() })
	if !drainUntil(func() bool { mu.Lock(); defer mu.Unlock(); return n == 1 }, 6*time.Second) {
		t.Fatal("live status never delivered")
	}
	for i := 0; i < 5; i++ {
		sink := &bufSinkMu{}
		unsub, err := sess.SubscribeWithAtomicReplay(sink, func(replay []byte) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		unsub()
	}
	time.Sleep(200 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if n != 1 {
		t.Errorf("status hook fired %d times, want 1: attach replayed through the scanner", n)
	}
}
