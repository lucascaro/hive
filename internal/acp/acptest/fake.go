// Package acptest is a scripted fake ACP agent for tests, so Go tests
// exercise the real protocol without Node or a real adapter. It is the
// Go counterpart of scripts/acp-probe/testdata/fake-agent.mjs.
//
// A test package re-executes its own test binary as the agent:
//
//	func TestMain(m *testing.M) {
//		if acptest.IsAgent() {
//			acptest.Main()
//			return
//		}
//		os.Exit(m.Run())
//	}
//
// and points the adapter Argv at os.Args[0] with Env(dir, flags...).
//
// Conversations live in <dir>/<sessionId>.jsonl, one {user, agent} line
// per turn, so a second process can session/load them like a real
// adapter. Every method received is appended to <dir>/calls.log as
// "<method> <sessionId>".
package acptest

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"
)

// EnvAgent marks the re-executed process as the fake agent.
const EnvAgent = "HIVE_FAKE_ACP"

// Behaviour flags.
const (
	// FlagPermission makes each prompt ask session/request_permission
	// for a tool call before replying; the reply names the option.
	FlagPermission = "permission"
	// FlagBlock makes each prompt wait until <dir>/release exists.
	FlagBlock = "block"
	// FlagNoLoad makes initialize report loadSession: false.
	FlagNoLoad = "no-load"
	// FlagSubmit makes each prompt ask permission for the submit tool of
	// the first MCP server it was given, as Claude's adapter names it
	// (mcp__<server>__submit_result), before its other work.
	FlagSubmit = "submit"
	// FlagEscalate makes each prompt switch, on its own, to the most
	// permissive mode (current_mode_update), as an adapter that
	// escalates would.
	FlagEscalate = "escalate"
	// FlagEscalateAfterAnswer adds Claude's exit-plan option
	// "exit-plan-accept-edits" to a FlagPermission request, and makes ANY
	// answer to it switch the mode to acceptEdits — as the exit-plan card
	// does for that option, and as a misbehaving adapter might for any.
	FlagEscalateAfterAnswer = "escalate-after-answer"
	// FlagModeOnNew makes session/new report a switch to the most
	// permissive mode before it returns, as an adapter settling in might.
	FlagModeOnNew = "mode-on-new"
	// FlagFewModes makes the fake advertise only Codex's "agent" mode.
	FlagFewModes = "few-modes"
	// FlagSetModeFails makes session/set_mode fail.
	FlagSetModeFails = "set-mode-fails"
	// FlagSetModeFailsAfterFirst makes every set_mode after the first
	// fail, so the start succeeds and a later reset does not.
	FlagSetModeFailsAfterFirst = "set-mode-fails-after-first"
)

// Modes is what the fake advertises, least permissive first: Claude's
// adapter's ids then Codex's, so a test can use either agent's def.
var Modes = []string{"plan", "default", "acceptEdits", "auto", "bypassPermissions", "read-only", "workspace-write", "agent", "agent-full-access"}

// IsAgent reports whether this process was started as the fake agent.
func IsAgent() bool { return os.Getenv(EnvAgent) == "1" }

// Env returns the environment variables that start the fake agent with
// its store in dir. Flags are any of the Flag* constants.
func Env(dir string, flags ...string) []string {
	return []string{EnvAgent + "=1", "HIVE_FAKE_ACP_DIR=" + dir, "HIVE_FAKE_ACP_FLAGS=" + strings.Join(flags, ",")}
}

// Turn is one stored exchange.
type Turn struct {
	User  string `json:"user"`
	Agent string `json:"agent"`
}

// AppendTurn adds a turn to a stored conversation, as a PTY takeover of
// the same conversation would.
func AppendTurn(dir, sessionID string, t Turn) error {
	f, err := os.OpenFile(filepath.Join(dir, sessionID+".jsonl"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	b, _ := json.Marshal(t)
	_, err = f.Write(append(b, '\n'))
	return err
}

// Calls returns the calls.log lines.
func Calls(dir string) []string {
	b, _ := os.ReadFile(filepath.Join(dir, "calls.log"))
	if s := strings.TrimSpace(string(b)); s != "" {
		return strings.Split(s, "\n")
	}
	return nil
}

// Release unblocks prompts started with FlagBlock.
func Release(dir string) error {
	return os.WriteFile(filepath.Join(dir, "release"), nil, 0o600)
}

type msg struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   any             `json:"error,omitempty"`
}

type fake struct {
	dir   string
	flags map[string]bool

	wmu sync.Mutex
	out *bufio.Writer

	mu      sync.Mutex
	next    int
	pending map[string]chan json.RawMessage
	servers map[string]string // sessionId -> first MCP server name
	setMode int               // set_mode calls so far
}

// Main runs the fake agent on stdin/stdout until stdin closes.
func Main() {
	f := &fake{
		dir:     os.Getenv("HIVE_FAKE_ACP_DIR"),
		flags:   map[string]bool{},
		out:     bufio.NewWriter(os.Stdout),
		pending: map[string]chan json.RawMessage{},
		servers: map[string]string{},
	}
	for fl := range strings.SplitSeq(os.Getenv("HIVE_FAKE_ACP_FLAGS"), ",") {
		f.flags[fl] = true
	}
	// A real adapter logs to stdout now and then; the client must skip it.
	fmt.Println("fake-acp: starting")
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for sc.Scan() {
		var m struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
			Result json.RawMessage `json:"result"`
		}
		if json.Unmarshal(sc.Bytes(), &m) != nil {
			continue
		}
		if m.Method == "" {
			f.mu.Lock()
			ch := f.pending[string(m.ID)]
			delete(f.pending, string(m.ID))
			f.mu.Unlock()
			if ch != nil {
				ch <- m.Result
			}
			continue
		}
		go f.handle(m.ID, m.Method, m.Params)
	}
}

func (f *fake) send(m msg) {
	m.JSONRPC = "2.0"
	b, _ := json.Marshal(m)
	f.wmu.Lock()
	f.out.Write(append(b, '\n'))
	f.out.Flush()
	f.wmu.Unlock()
}

func (f *fake) update(sid string, u map[string]any) {
	p, _ := json.Marshal(map[string]any{"sessionId": sid, "update": u})
	f.send(msg{Method: "session/update", Params: p})
}

func (f *fake) request(method string, params any) json.RawMessage {
	f.mu.Lock()
	f.next++
	id := fmt.Sprintf("%d", 1000+f.next)
	ch := make(chan json.RawMessage, 1)
	f.pending[id] = ch
	f.mu.Unlock()
	p, _ := json.Marshal(params)
	f.send(msg{ID: json.RawMessage(id), Method: method, Params: p})
	return <-ch
}

func (f *fake) log(method, sid string) {
	lf, err := os.OpenFile(filepath.Join(f.dir, "calls.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	fmt.Fprintf(lf, "%s %s\n", method, sid)
	lf.Close()
}

func (f *fake) turns(sid string) ([]Turn, bool) {
	b, err := os.ReadFile(filepath.Join(f.dir, sid+".jsonl"))
	if err != nil {
		return nil, false
	}
	var out []Turn
	for line := range strings.SplitSeq(strings.TrimSpace(string(b)), "\n") {
		var t Turn
		if json.Unmarshal([]byte(line), &t) == nil {
			out = append(out, t)
		}
	}
	return out, true
}

func (f *fake) handle(id json.RawMessage, method string, params json.RawMessage) {
	var p struct {
		SessionID string `json:"sessionId"`
		Prompt    []struct {
			Text string `json:"text"`
		} `json:"prompt"`
		ModeID     string `json:"modeId"`
		MCPServers []struct {
			Name    string   `json:"name"`
			Command string   `json:"command"`
			Args    []string `json:"args"`
			Env     []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"env"`
		} `json:"mcpServers"`
	}
	_ = json.Unmarshal(params, &p)
	if method == "session/set_mode" {
		// The mode is part of the line, so a test can see what was set.
		f.log(method, p.SessionID+" "+p.ModeID)
	} else {
		f.log(method, p.SessionID)
	}
	// "mcp <sessionId> <name> <command> <args…> <env names…>", one line
	// per server received, so a test can check new and load both got it.
	logMCP := func(sid string) {
		for i, m := range p.MCPServers {
			if i == 0 {
				f.mu.Lock()
				f.servers[sid] = m.Name
				f.mu.Unlock()
			}
			line := []string{m.Name, m.Command}
			line = append(line, m.Args...)
			for _, e := range m.Env {
				line = append(line, e.Name)
			}
			f.log("mcp", sid+" "+strings.Join(line, " "))
		}
	}
	modes := func() map[string]any {
		advertised := Modes
		if f.flags[FlagFewModes] {
			advertised = []string{"agent"}
		}
		var avail []any
		for _, m := range advertised {
			avail = append(avail, map[string]any{"id": m})
		}
		return map[string]any{"currentModeId": "agent", "availableModes": avail}
	}
	reply := func(res any) { f.send(msg{ID: id, Result: res}) }
	fail := func(code int, text string) {
		f.send(msg{ID: id, Error: map[string]any{"code": code, "message": text}})
	}

	switch method {
	case "initialize":
		// "env <name>…": the HIVE_* and PI_ACP_* variables the adapter
		// was started with (names only), so a test sees what reached it.
		var names []string
		for _, kv := range os.Environ() {
			k, _, _ := strings.Cut(kv, "=")
			if (strings.HasPrefix(k, "HIVE_") && !strings.HasPrefix(k, "HIVE_FAKE_ACP")) || strings.HasPrefix(k, "PI_ACP_") {
				names = append(names, k)
			}
		}
		slices.Sort(names)
		f.log("env", strings.Join(names, " "))
		reply(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": !f.flags[FlagNoLoad]}})
	case "session/new":
		sid := fmt.Sprintf("fake-%d", time.Now().UnixNano())
		if err := os.WriteFile(filepath.Join(f.dir, sid+".jsonl"), nil, 0o600); err != nil {
			fail(-32603, err.Error())
			return
		}
		logMCP(sid)
		if f.flags[FlagModeOnNew] {
			f.update(sid, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "bypassPermissions"})
		}
		reply(map[string]any{"sessionId": sid, "modes": modes()})
	case "session/load":
		turns, ok := f.turns(p.SessionID)
		if !ok {
			fail(-32002, "unknown session "+p.SessionID)
			return
		}
		logMCP(p.SessionID)
		for _, t := range turns {
			f.update(p.SessionID, map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": t.User}})
			f.update(p.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": t.Agent}})
		}
		// Like Codex's adapter, a load comes back in the adapter's own
		// default mode, whatever was set before.
		reply(map[string]any{"modes": modes()})
	case "session/set_mode":
		f.mu.Lock()
		f.setMode++
		n := f.setMode
		f.mu.Unlock()
		if f.flags[FlagSetModeFails] || (f.flags[FlagSetModeFailsAfterFirst] && n > 1) || !slices.Contains(Modes, p.ModeID) {
			fail(-32602, "Invalid params")
			return
		}
		reply(map[string]any{})
	case "session/prompt":
		text := ""
		for _, b := range p.Prompt {
			text += b.Text
		}
		f.update(p.SessionID, map[string]any{"sessionUpdate": "plan", "entries": []any{
			map[string]any{"content": "answer", "priority": "medium", "status": "in_progress"},
		}})
		// content is an ARRAY of ToolCallContent on tool calls, unlike a
		// message chunk's single block — the shape the client must decode.
		f.update(p.SessionID, map[string]any{"sessionUpdate": "tool_call", "toolCallId": "t1", "title": "Read file", "kind": "read", "status": "pending",
			"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "reading"}}}})
		answer := ""
		if f.flags[FlagSubmit] {
			f.mu.Lock()
			server := f.servers[p.SessionID]
			f.mu.Unlock()
			res := f.request("session/request_permission", map[string]any{
				"sessionId": p.SessionID,
				"toolCall":  map[string]any{"toolCallId": "s1", "title": "submit_result", "name": "mcp__" + server + "__submit_result"},
				"options": []any{
					map[string]any{"optionId": "allow-once", "name": "Yes", "kind": "allow_once"},
					map[string]any{"optionId": "reject", "name": "No", "kind": "reject_once"},
				},
			})
			answer += " [submit " + outcome(res) + "]"
		}
		if f.flags[FlagEscalate] {
			f.update(p.SessionID, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "bypassPermissions"})
		}
		if f.flags[FlagPermission] {
			options := []any{
				map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
				map[string]any{"optionId": "reject", "name": "Deny", "kind": "reject_once"},
			}
			if f.flags[FlagEscalateAfterAnswer] {
				options = append(options, map[string]any{"optionId": "exit-plan-accept-edits", "name": "Yes, and auto-accept edits", "kind": "allow_always"})
			}
			res := f.request("session/request_permission", map[string]any{
				"sessionId": p.SessionID,
				"toolCall":  map[string]any{"toolCallId": "t1", "title": "Read file"},
				"options":   options,
			})
			o := outcome(res)
			answer += " [" + o + "]"
			if f.flags[FlagEscalateAfterAnswer] {
				f.update(p.SessionID, map[string]any{"sessionUpdate": "current_mode_update", "currentModeId": "acceptEdits"})
			}
		}
		if f.flags[FlagBlock] {
			for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
				if _, err := os.Stat(filepath.Join(f.dir, "release")); err == nil {
					break
				}
			}
		}
		f.update(p.SessionID, map[string]any{"sessionUpdate": "tool_call_update", "toolCallId": "t1", "status": "completed",
			"content": []any{map[string]any{"type": "content", "content": map[string]any{"type": "text", "text": "done"}}}})
		agent := "echo: " + text + answer
		// Two chunks, so the client's coalescing is exercised.
		half := len(agent) / 2
		f.update(p.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": agent[:half]}})
		f.update(p.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": agent[half:]}})
		if err := AppendTurn(f.dir, p.SessionID, Turn{User: text, Agent: agent}); err != nil {
			fail(-32603, err.Error())
			return
		}
		reply(map[string]any{"stopReason": "end_turn"})
	default:
		fail(-32601, "unsupported method "+method)
	}
}

// outcome renders a permission response as "<outcome>:<optionId>".
func outcome(res json.RawMessage) string {
	var r struct {
		Outcome struct {
			Outcome  string `json:"outcome"`
			OptionID string `json:"optionId"`
		} `json:"outcome"`
	}
	_ = json.Unmarshal(res, &r)
	return r.Outcome.Outcome + ":" + r.Outcome.OptionID
}
