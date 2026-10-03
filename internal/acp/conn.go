// Package acp is Hive's client for the Agent Client Protocol: JSON-RPC
// 2.0, one message per line, over an adapter's stdio (spec 496).
//
// It speaks only what spike 492 proved against real adapters —
// initialize, session/new, session/load, session/prompt, the
// session/update notification and the session/request_permission
// request — and advertises no fs or terminal capability, so an adapter
// never calls back into Hive for those. There is no official Go SDK;
// coder/acp-go-sdk trails the schema, and this much is a page of code.
package acp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"strconv"
	"sync"
)

// ErrClosed is returned by calls in flight, or made after, the
// connection's reader hit EOF or an error.
var ErrClosed = errors.New("acp: connection closed")

// maxLine bounds one JSON-RPC message. A session/load replay of a long
// conversation arrives as many notifications, not one line, so this is
// generous rather than tight.
const maxLine = 16 << 20

// RPCError is a JSON-RPC error object.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("acp: rpc error %d: %s", e.Code, e.Message) }

// Standard JSON-RPC error codes this client produces.
const (
	CodeMethodNotFound = -32601
	CodeInternal       = -32603
)

type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// NotifyFunc receives notifications, in arrival order, on the reader
// goroutine. It must not block for long: replies queue behind it.
type NotifyFunc func(method string, params json.RawMessage)

// RequestFunc answers a request the agent sent. It runs on its own
// goroutine, so it may block — a permission request waits on the user.
type RequestFunc func(ctx context.Context, method string, params json.RawMessage) (any, *RPCError)

// Conn is one JSON-RPC connection.
type Conn struct {
	wmu sync.Mutex
	w   io.Writer

	mu      sync.Mutex
	next    int64
	pending map[string]chan message
	err     error

	notify  NotifyFunc
	request RequestFunc

	ctx    context.Context // cancelled when the reader ends
	cancel context.CancelFunc
	done   chan struct{}
}

// NewConn starts reading r and returns the connection. Either callback
// may be nil.
func NewConn(r io.Reader, w io.Writer, notify NotifyFunc, request RequestFunc) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	c := &Conn{
		w: w, pending: map[string]chan message{},
		notify: notify, request: request,
		ctx: ctx, cancel: cancel, done: make(chan struct{}),
	}
	go c.read(r)
	return c
}

// Done is closed when the reader ends.
func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) read(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), maxLine)
	for sc.Scan() {
		var m message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			// Adapters log to stdout now and then; one stray line must
			// not take the session down.
			log.Printf("acp: skipping non-JSON line (%d bytes)", len(sc.Bytes()))
			continue
		}
		switch {
		case m.Method != "" && len(m.ID) > 0:
			go c.serve(m)
		case m.Method != "":
			if c.notify != nil {
				c.notify(m.Method, m.Params)
			}
		case len(m.ID) > 0:
			c.mu.Lock()
			ch := c.pending[string(m.ID)]
			delete(c.pending, string(m.ID))
			c.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		}
	}
	err := sc.Err()
	if err == nil {
		err = io.EOF
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	c.cancel()
	close(c.done)
}

func (c *Conn) serve(m message) {
	reply := message{JSONRPC: "2.0", ID: m.ID}
	if c.request == nil {
		reply.Error = &RPCError{Code: CodeMethodNotFound, Message: "unsupported client method " + m.Method}
	} else if res, rerr := c.request(c.ctx, m.Method, m.Params); rerr != nil {
		reply.Error = rerr
	} else if b, err := json.Marshal(res); err != nil {
		reply.Error = &RPCError{Code: CodeInternal, Message: err.Error()}
	} else {
		reply.Result = b
	}
	_ = c.write(reply)
}

func (c *Conn) write(m message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_, err = c.w.Write(append(b, '\n'))
	return err
}

// Call sends a request and decodes its result into out (which may be
// nil). It returns when the reply arrives, ctx ends, or the connection
// closes.
func (c *Conn) Call(ctx context.Context, method string, params, out any) error {
	p, err := json.Marshal(params)
	if err != nil {
		return err
	}
	c.mu.Lock()
	if c.err != nil {
		c.mu.Unlock()
		return ErrClosed
	}
	c.next++
	id := strconv.FormatInt(c.next, 10)
	ch := make(chan message, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	if err := c.write(message{JSONRPC: "2.0", ID: json.RawMessage(id), Method: method, Params: p}); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return err
	}
	select {
	case m, ok := <-ch:
		if !ok {
			return ErrClosed
		}
		if m.Error != nil {
			return m.Error
		}
		if out == nil || len(m.Result) == 0 {
			return nil
		}
		return json.Unmarshal(m.Result, out)
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return ctx.Err()
	}
}
