package acp

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/lucascaro/hive/internal/acp/acptest"
)

func TestMain(m *testing.M) {
	if acptest.IsAgent() {
		acptest.Main()
		return
	}
	os.Exit(m.Run())
}

type recorder struct {
	mu      sync.Mutex
	updates []Update
}

func (r *recorder) handler(perm func(PermissionRequest) any) Handler {
	return Handler{
		Update: func(_ string, u Update) {
			r.mu.Lock()
			r.updates = append(r.updates, u)
			r.mu.Unlock()
		},
		Permission: func(_ context.Context, req PermissionRequest) any { return perm(req) },
	}
}

func (r *recorder) transcript() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var tr Transcript
	for _, u := range r.updates {
		tr.Apply(u, true)
	}
	var out []string
	for _, it := range tr.Snapshot() {
		if it.Text != "" {
			out = append(out, it.Kind+":"+it.Text)
		}
	}
	return out
}

func startFake(t *testing.T, dir string, h Handler, flags ...string) *Agent {
	t.Helper()
	a, err := Start(Spec{
		Argv: []string{os.Args[0]},
		Env:  append(AdapterEnv(os.Environ(), ""), acptest.Env(dir, flags...)...),
		Cwd:  dir,
	}, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(a.Close)
	return a
}

func ctx(t *testing.T) context.Context {
	c, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return c
}

func TestStartNewPromptRoundTrip(t *testing.T) {
	dir := t.TempDir()
	rec := &recorder{}
	a := startFake(t, dir, rec.handler(func(PermissionRequest) any { return Cancelled() }))
	if res, err := a.Initialize(ctx(t)); err != nil || !res.AgentCapabilities.LoadSession {
		t.Fatalf("Initialize = %+v, %v", res, err)
	}
	ns, err := a.NewSession(ctx(t), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	stop, err := a.Prompt(ctx(t), ns.SessionID, "hello")
	if err != nil || stop != "end_turn" {
		t.Fatalf("Prompt = %q, %v", stop, err)
	}
	if got := rec.transcript(); !slices.Equal(got, []string{"agent:echo: hello"}) {
		t.Errorf("transcript = %q", got)
	}
}

func TestPermissionRequestReachesHandler(t *testing.T) {
	dir := t.TempDir()
	rec := &recorder{}
	var asked PermissionRequest
	a := startFake(t, dir, rec.handler(func(req PermissionRequest) any {
		asked = req
		return Selected("allow")
	}), acptest.FlagPermission)
	if _, err := a.Initialize(ctx(t)); err != nil {
		t.Fatal(err)
	}
	ns, _ := a.NewSession(ctx(t), dir, nil)
	if _, err := a.Prompt(ctx(t), ns.SessionID, "go"); err != nil {
		t.Fatal(err)
	}
	if asked.ToolCall.ToolCallID != "t1" || len(asked.Options) != 2 {
		t.Errorf("permission request = %+v", asked)
	}
	if got := rec.transcript(); len(got) != 1 || !strings.Contains(got[0], "[selected:allow]") {
		t.Errorf("transcript = %q, want the agent to see the allow answer", got)
	}
}

// A second adapter process reopens the conversation and replays it,
// which is what a daemon restart relies on.
func TestLoadReplaysHistory(t *testing.T) {
	dir := t.TempDir()
	a := startFake(t, dir, Handler{})
	a.Initialize(ctx(t))
	ns, _ := a.NewSession(ctx(t), dir, nil)
	a.Prompt(ctx(t), ns.SessionID, "one")
	a.Close()

	rec := &recorder{}
	b := startFake(t, dir, rec.handler(nil))
	b.Initialize(ctx(t))
	if err := b.LoadSession(ctx(t), ns.SessionID, dir, nil); err != nil {
		t.Fatal(err)
	}
	if got := rec.transcript(); !slices.Equal(got, []string{"user:one", "agent:echo: one"}) {
		t.Errorf("replayed = %q", got)
	}
}

func TestEnvStripsClaudeAndHiveVars(t *testing.T) {
	got := AdapterEnv([]string{
		"CLAUDECODE=1", "CLAUDE_CODE_CHILD_SESSION=1", "HIVE_SOCKET=/s", "HIVE_SESSION_ID=x",
		"CLAUDE_CODE_ENABLE_TODO_TOOLS=1", "PATH=/old", "HOME=/h",
	}, "/login/bin")
	want := []string{"CLAUDE_CODE_ENABLE_TODO_TOOLS=1", "HOME=/h", "PATH=/login/bin"}
	if !slices.Equal(got, want) {
		t.Errorf("AdapterEnv = %q, want %q", got, want)
	}
	if got := AdapterEnv([]string{"PATH=/keep"}, ""); !slices.Equal(got, []string{"PATH=/keep"}) {
		t.Errorf("AdapterEnv with no login PATH = %q, want PATH kept", got)
	}
}

// Close must end a turn the adapter is stuck in, and the call waiting
// on it, instead of leaking the process.
func TestCloseEndsHungTurn(t *testing.T) {
	dir := t.TempDir()
	a := startFake(t, dir, Handler{}, acptest.FlagBlock)
	a.Initialize(ctx(t))
	ns, _ := a.NewSession(ctx(t), dir, nil)
	errc := make(chan error, 1)
	go func() { _, err := a.Prompt(context.Background(), ns.SessionID, "wait"); errc <- err }()
	time.Sleep(200 * time.Millisecond)
	a.Close()
	select {
	case err := <-errc:
		if err == nil {
			t.Error("Prompt returned nil after Close, want an error")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Prompt still blocked after Close")
	}
	select {
	case <-a.Done():
	default:
		t.Error("Done not closed after Close returned")
	}
}

func TestStartMissingBinary(t *testing.T) {
	if _, err := Start(Spec{Argv: []string{"/nonexistent/adapter"}}, Handler{}); err == nil {
		t.Error("Start of a missing binary succeeded")
	}
}
