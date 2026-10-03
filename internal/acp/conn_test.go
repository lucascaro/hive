package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"
	"time"
)

// peer is the far end of a Conn under test: it reads what the Conn
// writes and writes raw lines back.
type peer struct {
	in  *bufio.Scanner
	out io.Writer
}

func pipeConn(t *testing.T, notify NotifyFunc, request RequestFunc) (*Conn, *peer) {
	t.Helper()
	toConnR, toConnW := io.Pipe()
	fromConnR, fromConnW := io.Pipe()
	c := NewConn(toConnR, fromConnW, notify, request)
	t.Cleanup(func() { toConnW.Close(); fromConnR.Close() })
	return c, &peer{in: bufio.NewScanner(fromConnR), out: toConnW}
}

func (p *peer) read(t *testing.T) message {
	t.Helper()
	if !p.in.Scan() {
		t.Fatal("peer: no message")
	}
	var m message
	if err := json.Unmarshal(p.in.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func (p *peer) write(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(p.out, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

// Replies may arrive in any order; each must reach its own caller.
func TestCallMatchesReplyByID(t *testing.T) {
	c, p := pipeConn(t, nil, nil)
	type res struct {
		v   string
		err error
	}
	a, b := make(chan res, 1), make(chan res, 1)
	go func() {
		var out struct{ V string }
		err := c.Call(context.Background(), "a", nil, &out)
		a <- res{out.V, err}
	}()
	first := p.read(t)
	go func() {
		var out struct{ V string }
		err := c.Call(context.Background(), "b", nil, &out)
		b <- res{out.V, err}
	}()
	second := p.read(t)
	p.write(t, `{"jsonrpc":"2.0","id":`+string(second.ID)+`,"result":{"V":"`+second.Method+`"}}`)
	p.write(t, `{"jsonrpc":"2.0","id":`+string(first.ID)+`,"result":{"V":"`+first.Method+`"}}`)
	if got := <-a; got.err != nil || got.v != "a" {
		t.Errorf("call a = %+v, want a", got)
	}
	if got := <-b; got.err != nil || got.v != "b" {
		t.Errorf("call b = %+v, want b", got)
	}
}

func TestHandleUnknownMethodReturnsMethodNotFound(t *testing.T) {
	_, p := pipeConn(t, nil, nil)
	p.write(t, `{"jsonrpc":"2.0","id":7,"method":"fs/read_text_file","params":{}}`)
	m := p.read(t)
	if string(m.ID) != "7" || m.Error == nil || m.Error.Code != CodeMethodNotFound {
		t.Errorf("reply = %+v, want method-not-found for id 7", m)
	}
}

// Adapters print to stdout occasionally; one stray line must not end
// the connection or swallow the next message.
func TestNonJSONLineIsSkipped(t *testing.T) {
	got := make(chan string, 1)
	_, p := pipeConn(t, func(method string, _ json.RawMessage) { got <- method }, nil)
	p.write(t, "npm WARN something")
	p.write(t, `{"jsonrpc":"2.0","method":"session/update","params":{}}`)
	select {
	case m := <-got:
		if m != "session/update" {
			t.Errorf("notify = %q", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("notification after a non-JSON line never arrived")
	}
}

func TestCallFailsWhenPeerCloses(t *testing.T) {
	c, p := pipeConn(t, nil, nil)
	errc := make(chan error, 1)
	go func() { errc <- c.Call(context.Background(), "x", nil, nil) }()
	p.read(t)
	p.out.(io.Closer).Close()
	if err := <-errc; !errors.Is(err, ErrClosed) {
		t.Errorf("Call after peer EOF = %v, want ErrClosed", err)
	}
	<-c.Done()
	if err := c.Call(context.Background(), "y", nil, nil); !errors.Is(err, ErrClosed) {
		t.Errorf("Call on a closed conn = %v, want ErrClosed", err)
	}
}

func TestRequestErrorIsSentBack(t *testing.T) {
	_, p := pipeConn(t, nil, func(context.Context, string, json.RawMessage) (any, *RPCError) {
		return nil, &RPCError{Code: 42, Message: "nope"}
	})
	p.write(t, `{"jsonrpc":"2.0","id":"r1","method":"session/request_permission","params":{}}`)
	if m := p.read(t); m.Error == nil || m.Error.Code != 42 || string(m.ID) != `"r1"` {
		t.Errorf("reply = %+v, want error 42 for id r1", m)
	}
}
