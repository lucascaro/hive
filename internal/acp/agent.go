package acp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/lucascaro/hive/internal/proc"
)

// Spec is how to start one adapter process.
type Spec struct {
	Argv []string
	Env  []string // complete environment; build it with AdapterEnv
	Cwd  string
}

// Handler receives what the agent sends. Both funcs may be nil.
type Handler struct {
	// Update gets each session/update in arrival order, on the reader
	// goroutine.
	Update func(sessionID string, u Update)
	// Permission answers session/request_permission; return Selected
	// or Cancelled. It may block until the user answers. ctx ends when
	// the adapter goes away.
	Permission func(ctx context.Context, req PermissionRequest) any
}

// Agent is one running adapter process and its connection.
type Agent struct {
	cmd    *exec.Cmd
	stdin  *os.File
	conn   *Conn
	stderr *tail

	done    chan struct{}
	waitErr error
	close   sync.Once
}

// closeGrace is how long Close lets an adapter exit on stdin EOF and
// SIGTERM before it SIGKILLs the group.
var closeGrace = 2 * time.Second

// strippedEnv are inherited variables an adapter must not see. The
// first two make a nested Claude Code stop saving its transcript —
// which would leave nothing for session/load or a PTY takeover to
// resume (spike 492, probe.mjs). HIVE_* are the daemon's own session
// variables; an adapter is not a Hive session's child.
var strippedEnv = []string{"CLAUDECODE=", "CLAUDE_CODE_CHILD_SESSION="}

// AdapterEnv returns base without the variables an adapter must not
// inherit, with PATH replaced by path when path is non-empty. Every
// other variable — user preferences such as CLAUDE_CODE_ENABLE_TODO_TOOLS
// included — passes through.
func AdapterEnv(base []string, path string) []string {
	out := make([]string, 0, len(base)+1)
	for _, kv := range base {
		if strings.HasPrefix(kv, "HIVE_") || (path != "" && strings.HasPrefix(kv, "PATH=")) {
			continue
		}
		drop := false
		for _, p := range strippedEnv {
			if strings.HasPrefix(kv, p) {
				drop = true
				break
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	if path != "" {
		out = append(out, "PATH="+path)
	}
	return out
}

// Start launches the adapter in its own process group and connects to
// it. It does not initialize; call Initialize next.
func Start(spec Spec, h Handler) (*Agent, error) {
	if len(spec.Argv) == 0 {
		return nil, errors.New("acp: empty adapter command")
	}
	// exec resolves a bare name on the DAEMON's PATH, not on the one in
	// spec.Env — and a Finder-launched hived has no npx on its own PATH.
	// Resolve it where the adapter will actually run.
	name := spec.Argv[0]
	if !filepath.IsAbs(name) {
		if p := proc.LookPathIn(envPATH(spec.Env), name); p != "" {
			name = p
		}
	}
	cmd := proc.Command(name, spec.Argv[1:]...)
	cmd.Dir = spec.Cwd
	cmd.Env = spec.Env
	proc.OwnGroup(cmd)

	// Plain pipes, all three, rather than exec's own: Wait waits for
	// exec's copy goroutines, and a grandchild (npx → node → CLI) can
	// hold an inherited pipe past the leader's exit, so Wait would never
	// return for a dead adapter. With os.Pipe, Wait returns when the
	// leader exits, the group kill after it reaps the rest, and the
	// readers drain what is left on their own.
	var files []*os.File
	pipe := func() (r, w *os.File, err error) {
		r, w, err = os.Pipe()
		files = append(files, r, w)
		return
	}
	closeAll := func() {
		for _, f := range files {
			f.Close()
		}
	}
	inR, inW, err := pipe()
	if err != nil {
		closeAll()
		return nil, err
	}
	outR, outW, err := pipe()
	if err != nil {
		closeAll()
		return nil, err
	}
	errR, errW, err := pipe()
	if err != nil {
		closeAll()
		return nil, err
	}
	a := &Agent{cmd: cmd, stdin: inW, stderr: &tail{}, done: make(chan struct{})}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = inR, outW, errW
	if err := cmd.Start(); err != nil {
		closeAll()
		return nil, fmt.Errorf("acp: start %s: %w", spec.Argv[0], err)
	}
	inR.Close()
	outW.Close()
	errW.Close()
	go func() {
		_, _ = io.Copy(a.stderr, errR)
		errR.Close()
	}()

	a.conn = NewConn(outR, inW, func(method string, params json.RawMessage) {
		if method != MethodSessionUpdate || h.Update == nil {
			return
		}
		var n SessionNotification
		var u Update
		if err := json.Unmarshal(params, &n); err != nil {
			log.Printf("acp: dropping undecodable session/update: %v", err)
			return
		}
		if err := json.Unmarshal(n.Update, &u); err != nil {
			log.Printf("acp: dropping undecodable session/update: %v", err)
			return
		}
		h.Update(n.SessionID, u)
	}, func(ctx context.Context, method string, params json.RawMessage) (any, *RPCError) {
		if method != MethodRequestPermission || h.Permission == nil {
			return nil, &RPCError{Code: CodeMethodNotFound, Message: "unsupported client method " + method}
		}
		var req PermissionRequest
		if err := json.Unmarshal(params, &req); err != nil {
			return nil, &RPCError{Code: CodeInternal, Message: err.Error()}
		}
		return h.Permission(ctx, req), nil
	})

	go func() {
		a.waitErr = cmd.Wait()
		// The adapter is gone; anything it left in its group goes too,
		// which also closes the pipe a grandchild was holding.
		_ = proc.KillTree(cmd.Process)
		_ = inW.Close()
		// The group kill closes stdout unless a grandchild left the
		// group (setsid) still holding it; don't wait on that forever.
		select {
		case <-a.conn.Done():
		case <-time.After(closeGrace):
			outR.Close() // ends the reader with an error
			<-a.conn.Done()
		}
		outR.Close()
		close(a.done)
	}()
	return a, nil
}

// Done is closed once the adapter process has exited and its output is
// drained.
func (a *Agent) Done() <-chan struct{} { return a.done }

// LastError returns the tail of what the adapter wrote to stderr, for
// showing the user why a session died.
func (a *Agent) LastError() string {
	s := strings.TrimSpace(a.stderr.String())
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	if s == "" {
		select {
		case <-a.done: // waitErr is written before done closes
			if a.waitErr != nil {
				s = a.waitErr.Error()
			}
		default:
		}
	}
	return s
}

// Close stops the adapter: stdin EOF and SIGTERM first, then SIGKILL
// for the whole group after closeGrace. It returns once Done is closed.
func (a *Agent) Close() {
	a.close.Do(func() {
		_ = a.stdin.Close()
		proc.TermTree(a.cmd.Process)
		select {
		case <-a.done:
		case <-time.After(closeGrace):
			_ = proc.KillTree(a.cmd.Process)
		}
	})
	<-a.done
}

// Initialize negotiates the protocol. Hive advertises no fs and no
// terminal capability.
func (a *Agent) Initialize(ctx context.Context) (InitializeResult, error) {
	var res InitializeResult
	err := a.conn.Call(ctx, MethodInitialize, initializeParams{ProtocolVersion: ProtocolVersion}, &res)
	return res, err
}

// NewSession starts a conversation in cwd.
func (a *Agent) NewSession(ctx context.Context, cwd string, mcp []MCPServer) (NewSessionResult, error) {
	var res NewSessionResult
	err := a.conn.Call(ctx, MethodSessionNew, newSessionParams{Cwd: cwd, MCPServers: nonNil(mcp)}, &res)
	if err == nil && res.SessionID == "" {
		err = errors.New("acp: session/new returned no sessionId")
	}
	return res, err
}

// LoadSession reopens conversation id; the agent replays its history
// as session/update notifications before the call returns.
func (a *Agent) LoadSession(ctx context.Context, id, cwd string, mcp []MCPServer) error {
	return a.conn.Call(ctx, MethodSessionLoad, loadSessionParams{SessionID: id, Cwd: cwd, MCPServers: nonNil(mcp)}, nil)
}

// Prompt sends one user turn and blocks until the turn ends, returning
// ACP's stopReason (end_turn, cancelled, …).
func (a *Agent) Prompt(ctx context.Context, id, text string) (string, error) {
	var res promptResult
	err := a.conn.Call(ctx, MethodSessionPrompt, promptParams{
		SessionID: id,
		Prompt:    []ContentBlock{{Type: "text", Text: text}},
	}, &res)
	return res.StopReason, err
}

// envPATH returns the PATH entry of env.
func envPATH(env []string) string {
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "PATH="); ok {
			return v
		}
	}
	return ""
}

// nonNil keeps "mcpServers": [] on the wire: adapters reject null.
func nonNil(s []MCPServer) []MCPServer {
	if s == nil {
		return []MCPServer{}
	}
	return s
}

// tail keeps the last tailMax bytes written to it.
type tail struct {
	mu  sync.Mutex
	buf []byte
}

const tailMax = 4 << 10

func (t *tail) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if len(t.buf) > tailMax {
		t.buf = append([]byte(nil), t.buf[len(t.buf)-tailMax:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
