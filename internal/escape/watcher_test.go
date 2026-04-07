package escape

import (
	"regexp"
	"testing"

	"github.com/lucascaro/hive/internal/mux"
	"github.com/lucascaro/hive/internal/mux/muxtest"
	"github.com/lucascaro/hive/internal/state"
)

// setupMockBackend installs a muxtest.MockBackend as the active backend and
// returns it for test configuration. Tests must call this before WatchStatuses.
func setupMockBackend(t *testing.T) *muxtest.MockBackend {
	t.Helper()
	mb := muxtest.New()
	mux.SetBackend(mb)
	return mb
}

func TestDetectStatus_TitleWaitMatch(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "some content")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "some content"} // same content = stable
	stable := map[string]int{"s1": 5}
	det := map[string]SessionDetectionCtx{
		"s1": {
			WaitTitleRe: regexp.MustCompile(`^waiting`),
			StableTicks: 2,
		},
	}
	titles := map[string]string{"hive:0": "waiting for confirmation"}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm, ok := msg.(StatusesDetectedMsg)
	if !ok {
		t.Fatalf("expected StatusesDetectedMsg, got %T", msg)
	}
	if sdm.Statuses["s1"] != state.StatusWaiting {
		t.Errorf("expected StatusWaiting, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_TitleRunMatch(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "some content")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "some content"}
	stable := map[string]int{"s1": 5}
	det := map[string]SessionDetectionCtx{
		"s1": {
			RunTitleRe:  regexp.MustCompile(`^[⠁-⠿]`),
			StableTicks: 2,
		},
	}
	titles := map[string]string{"hive:0": "⠋ Working on task"}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusRunning {
		t.Errorf("expected StatusRunning, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_ContentChangedOverridesStaleTitle(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "new output from agent")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "old output"}
	stable := map[string]int{"s1": 0}
	det := map[string]SessionDetectionCtx{
		"s1": {
			WaitTitleRe: regexp.MustCompile(`^waiting`),
			StableTicks: 2,
		},
	}
	// Title says waiting (stale), but content is changing.
	titles := map[string]string{"hive:0": "waiting for something"}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusRunning {
		t.Errorf("content change should override stale title: expected StatusRunning, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_IdlePromptMatch_Idle(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "previous output\n> ")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "previous output\n> "} // stable
	stable := map[string]int{"s1": 5}
	det := map[string]SessionDetectionCtx{
		"s1": {
			IdlePromptRe: regexp.MustCompile(`^> `),
			StableTicks:  2,
		},
	}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusIdle {
		t.Errorf("at prompt should be idle: expected StatusIdle, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_IdlePromptNotMatch_Waiting(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "Do you want to proceed? (y/n)")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "Do you want to proceed? (y/n)"} // stable
	stable := map[string]int{"s1": 5}
	det := map[string]SessionDetectionCtx{
		"s1": {
			IdlePromptRe: regexp.MustCompile(`^> `),
			StableTicks:  2,
		},
	}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusWaiting {
		t.Errorf("not at prompt should be waiting: expected StatusWaiting, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_ContentChanged(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "new content")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "old content"} // different = changed
	stable := map[string]int{"s1": 0}
	det := map[string]SessionDetectionCtx{}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusRunning {
		t.Errorf("expected StatusRunning, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_StablePromptMatch(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "line1\nline2\n>>> ")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "line1\nline2\n>>> "} // same = stable
	stable := map[string]int{"s1": 3}                      // past debounce
	det := map[string]SessionDetectionCtx{
		"s1": {
			WaitPromptRe: regexp.MustCompile(`^>>> `),
			StableTicks:  2,
		},
	}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusWaiting {
		t.Errorf("expected StatusWaiting, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_StableNoSignals_Idle(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "some output")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "some output"} // same = stable
	stable := map[string]int{"s1": 5}               // past debounce
	det := map[string]SessionDetectionCtx{
		"s1": {StableTicks: 2},
	}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusIdle {
		t.Errorf("expected StatusIdle, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_Debounce(t *testing.T) {
	mb := setupMockBackend(t)
	mb.SetPaneContent("hive:0", "content")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "content"} // same = stable
	stable := map[string]int{"s1": 0}          // just became stable, below threshold
	det := map[string]SessionDetectionCtx{
		"s1": {StableTicks: 3},
	}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusRunning {
		t.Errorf("expected StatusRunning during debounce, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_IdlePromptWithANSITrailingLines(t *testing.T) {
	mb := setupMockBackend(t)
	// Simulate tmux output: prompt line followed by ANSI-only blank lines
	mb.SetPaneContent("hive:0", "previous output\n> \n\x1b[0m\n\x1b[0m")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "previous output\n> \n\x1b[0m\n\x1b[0m"} // stable
	stable := map[string]int{"s1": 5}
	det := map[string]SessionDetectionCtx{
		"s1": {
			IdlePromptRe: regexp.MustCompile(`^> `),
			StableTicks:  2,
		},
	}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusIdle {
		t.Errorf("prompt with ANSI trailing lines should be idle: expected StatusIdle, got %q", sdm.Statuses["s1"])
	}
}

func TestDetectStatus_WaitingWithANSITrailingLines(t *testing.T) {
	mb := setupMockBackend(t)
	// Simulate tmux output: question followed by ANSI-only blank lines
	mb.SetPaneContent("hive:0", "Allow tool_use? (y/n)\n\x1b[0m\n\x1b[0m")

	targets := map[string]string{"s1": "hive:0"}
	prev := map[string]string{"s1": "Allow tool_use? (y/n)\n\x1b[0m\n\x1b[0m"} // stable
	stable := map[string]int{"s1": 5}
	det := map[string]SessionDetectionCtx{
		"s1": {
			IdlePromptRe: regexp.MustCompile(`^> `),
			StableTicks:  2,
		},
	}
	titles := map[string]string{}

	cmd := WatchStatuses(targets, prev, stable, det, titles, 0)
	msg := cmd()
	sdm := msg.(StatusesDetectedMsg)
	if sdm.Statuses["s1"] != state.StatusWaiting {
		t.Errorf("question with ANSI trailing lines should be waiting: expected StatusWaiting, got %q", sdm.Statuses["s1"])
	}
}

// --- matchesLastLine tests ---

func TestMatchesLastLine_SimpleMatch(t *testing.T) {
	re := regexp.MustCompile(`^>>> `)
	if !matchesLastLine("output\n>>> ", re) {
		t.Error("expected match")
	}
}

func TestMatchesLastLine_EmptyContent(t *testing.T) {
	re := regexp.MustCompile(`^>>> `)
	if matchesLastLine("", re) {
		t.Error("expected no match on empty content")
	}
}

func TestMatchesLastLine_TrailingWhitespace(t *testing.T) {
	re := regexp.MustCompile(`^>>> `)
	if !matchesLastLine("output\n>>> \n\n  \n", re) {
		t.Error("expected match skipping trailing blank lines")
	}
}

func TestMatchesLastLine_WithANSI(t *testing.T) {
	re := regexp.MustCompile(`^>>> `)
	// ANSI color codes wrapping the prompt
	if !matchesLastLine("output\n\x1b[32m>>> \x1b[0m", re) {
		t.Error("expected match after ANSI stripping")
	}
}

func TestLastNonEmptyLine(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    string
	}{
		{"simple", "a\nb\nc", "c"},
		{"trailing blanks", "a\nb\n\n  \n", "b"},
		{"empty", "", ""},
		{"only blanks", "\n  \n\t\n", ""},
		{"single line", "hello", "hello"},
		{"preserves whitespace", "a\n>>> ", ">>> "},
		{"ansi-only trailing", "prompt line\n\x1b[0m\n\x1b[0m", "prompt line"},
		{"ansi-only all", "\x1b[0m\n\x1b[0m", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := lastNonEmptyLine(tt.content)
			if got != tt.want {
				t.Errorf("lastNonEmptyLine(%q) = %q, want %q", tt.content, got, tt.want)
			}
		})
	}
}

func TestStripANSI(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"\x1b[32mhello\x1b[0m", "hello"},
		{"no escapes", "no escapes"},
		{"\x1b[1;34m>>> \x1b[0m", ">>> "},
	}
	for _, tt := range tests {
		got := stripANSI(tt.input)
		if got != tt.want {
			t.Errorf("stripANSI(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
