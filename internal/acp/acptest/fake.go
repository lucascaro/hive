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
)

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
}

// Main runs the fake agent on stdin/stdout until stdin closes.
func Main() {
	f := &fake{
		dir:     os.Getenv("HIVE_FAKE_ACP_DIR"),
		flags:   map[string]bool{},
		out:     bufio.NewWriter(os.Stdout),
		pending: map[string]chan json.RawMessage{},
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
	}
	_ = json.Unmarshal(params, &p)
	f.log(method, p.SessionID)
	reply := func(res any) { f.send(msg{ID: id, Result: res}) }
	fail := func(code int, text string) {
		f.send(msg{ID: id, Error: map[string]any{"code": code, "message": text}})
	}

	switch method {
	case "initialize":
		reply(map[string]any{"protocolVersion": 1, "agentCapabilities": map[string]any{"loadSession": true}})
	case "session/new":
		sid := fmt.Sprintf("fake-%d", time.Now().UnixNano())
		if err := os.WriteFile(filepath.Join(f.dir, sid+".jsonl"), nil, 0o600); err != nil {
			fail(-32603, err.Error())
			return
		}
		reply(map[string]any{"sessionId": sid, "modes": map[string]any{
			"currentModeId": "default", "availableModes": []any{map[string]any{"id": "default"}},
		}})
	case "session/load":
		turns, ok := f.turns(p.SessionID)
		if !ok {
			fail(-32002, "unknown session "+p.SessionID)
			return
		}
		for _, t := range turns {
			f.update(p.SessionID, map[string]any{"sessionUpdate": "user_message_chunk", "content": map[string]any{"type": "text", "text": t.User}})
			f.update(p.SessionID, map[string]any{"sessionUpdate": "agent_message_chunk", "content": map[string]any{"type": "text", "text": t.Agent}})
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
		if f.flags[FlagPermission] {
			res := f.request("session/request_permission", map[string]any{
				"sessionId": p.SessionID,
				"toolCall":  map[string]any{"toolCallId": "t1", "title": "Read file"},
				"options": []any{
					map[string]any{"optionId": "allow", "name": "Allow", "kind": "allow_once"},
					map[string]any{"optionId": "reject", "name": "Deny", "kind": "reject_once"},
				},
			})
			var r struct {
				Outcome struct {
					Outcome  string `json:"outcome"`
					OptionID string `json:"optionId"`
				} `json:"outcome"`
			}
			_ = json.Unmarshal(res, &r)
			answer = " [" + r.Outcome.Outcome + ":" + r.Outcome.OptionID + "]"
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
