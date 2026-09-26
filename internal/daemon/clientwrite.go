package daemon

import (
	"bytes"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// The daemon never blocks on a client (DESIGN.md hard rule; see
// docs/design-docs/slow-client-policy.md). Every write to a client conn
// is bounded by clientWriteTimeout, and attach output goes through a
// bounded queue so no socket write ever happens under Session.mu. A
// client that stops reading is disconnected; every client already
// reconnects and re-snapshots on EOF.

// clientWriteTimeout bounds each write to a client conn, in
// nanoseconds. A healthy client drains a frame in microseconds; one
// that has not taken a frame in this long has stopped reading and is
// hung up on.
//
// attachBacklogLimit caps the live output queued for one attach client
// beyond any replay still in flight, in bytes. Far above what a reading
// client ever accumulates; a client this far behind is disconnected
// rather than allowed to hold the session's output in memory.
//
// Atomics, not consts, so the tests can shrink them while daemon
// goroutines from the same test are still reading them.
var clientWriteTimeout, attachBacklogLimit atomic.Int64

func init() {
	clientWriteTimeout.Store(int64(10 * time.Second))
	attachBacklogLimit.Store(8 << 20)
}

func writeTimeout() time.Duration { return time.Duration(clientWriteTimeout.Load()) }

var (
	errSinkClosed  = errors.New("daemon: attach sink closed")
	errSinkBacklog = errors.New("daemon: attach client fell too far behind")
)

// encodeJSON renders one JSON frame into memory, so a marshal error is
// reported before anything touches the conn and the frame goes out in a
// single Write.
func encodeJSON(t wire.FrameType, v any) ([]byte, error) {
	var b bytes.Buffer
	if err := wire.WriteJSON(&b, t, v); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// encodeFrame renders one raw frame into a fresh buffer. The copy is
// load-bearing for frameSink: the PTY readLoop reuses its read buffer.
func encodeFrame(t wire.FrameType, p []byte) ([]byte, error) {
	var b bytes.Buffer
	b.Grow(len(p) + 5)
	if err := wire.WriteFrame(&b, t, p); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// writeBounded writes one JSON frame under a fresh write deadline. The
// deadline is set immediately before the write, never at accept:
// ModeCreate runs a synchronous `git worktree add` before its WELCOME,
// which a deadline armed at accept could expire under. A failed write
// closes the conn: a timed-out write may have left half a frame, so
// the stream is unusable either way.
func writeBounded(conn net.Conn, timeout time.Duration, t wire.FrameType, v any) error {
	b, err := encodeJSON(t, v)
	if err != nil {
		return err
	}
	_ = conn.SetWriteDeadline(time.Now().Add(timeout))
	if _, err := conn.Write(b); err != nil {
		_ = conn.Close()
		return err
	}
	return nil
}

// sinkItem is one encoded frame waiting in a frameSink's queue.
type sinkItem struct {
	b      []byte
	replay bool
}

// frameSink is a session.Sink over an attach conn. Session.deliver and
// the atomic-replay helpers call it while holding Session.mu, so it
// never touches the conn from those calls: Write and writeReplay only
// encode and enqueue, and one writer goroutine drains the queue to the
// socket. Because both enqueue paths still run under Session.mu, the
// FIFO preserves the replay/live ordering SubscribeWithAtomicReplay
// guarantees.
//
// A client that falls further behind than the backlog limit, or does
// not take a frame within the write timeout, is disconnected.
type frameSink struct {
	conn    net.Conn
	timeout time.Duration
	limit   int

	mu     sync.Mutex
	queue  []sinkItem
	queued int // bytes in queue, including a batch being written
	// replayOwed is the replay bytes still queued. It raises the limit
	// while a replay drains (a replay can be up to the session ring's
	// size and must always fit) and falls back to zero once it is out.
	replayOwed int
	closing    bool // graceful: drain what is queued, then close
	stopped    bool // hard: close now, drop the queue
	wake       chan struct{}
	done       chan struct{}
}

// newFrameSink starts the writer goroutine. The limits are captured
// here, not re-read per write, so a test shrinking them cannot race a
// writer still draining from an earlier test. Callers must stop() it.
func newFrameSink(conn net.Conn) *frameSink {
	f := &frameSink{
		conn:    conn,
		timeout: writeTimeout(),
		limit:   int(attachBacklogLimit.Load()),
		wake:    make(chan struct{}, 1),
		done:    make(chan struct{}),
	}
	go f.run()
	return f
}

// Write queues one chunk of live PTY output. It never blocks on the
// conn. An error means the sink is gone and the session should drop it.
func (f *frameSink) Write(p []byte) (int, error) {
	b, err := encodeFrame(wire.FrameData, p)
	if err != nil {
		return 0, err
	}
	if err := f.enqueue([]sinkItem{{b: b}}, len(b), false); err != nil {
		return 0, err
	}
	return len(p), nil
}

// writeReplay queues Begin → chunked replay bytes → Done as one unit.
// It runs under Session.mu via SubscribeWithAtomicReplay /
// EmitAtomicReplay, so no live chunk can be queued between Begin and
// Done. Without that, a live byte could reach xterm in `live` phase and
// be wiped by the term.reset() that Begin triggers.
//
// chunk is the max payload size per FrameData; pass 16<<10 to match
// existing snapshot chunking.
func (f *frameSink) writeReplay(replay []byte, chunk int) error {
	items := make([]sinkItem, 0, len(replay)/chunk+3)
	n := 0
	add := func(b []byte, err error) error {
		if err != nil {
			return err
		}
		items = append(items, sinkItem{b: b, replay: true})
		n += len(b)
		return nil
	}
	if err := add(encodeJSON(wire.FrameEvent, wire.Event{Kind: wire.EventScrollbackReplayBegin})); err != nil {
		return err
	}
	for len(replay) > 0 {
		c := min(chunk, len(replay))
		if err := add(encodeFrame(wire.FrameData, replay[:c])); err != nil {
			return err
		}
		replay = replay[c:]
	}
	if err := add(encodeJSON(wire.FrameEvent, wire.Event{Kind: wire.EventScrollbackReplayDone})); err != nil {
		return err
	}
	return f.enqueue(items, n, true)
}

// enqueue appends items atomically. Live output past the limit
// disconnects the client. A replay is always accepted when none is
// outstanding, and widens the limit by its own size until it drains. A
// replay requested while another is still queued gets no allowance of
// its own: it counts against the backlog like live output. Without that,
// a client could send REQUEST_REPLAY in a loop without reading and have
// each one queue a full ring's worth of bytes. A healthy client drains
// a replay in milliseconds and never has two queued.
func (f *frameSink) enqueue(items []sinkItem, n int, replay bool) error {
	f.mu.Lock()
	if f.stopped || f.closing {
		f.mu.Unlock()
		return errSinkClosed
	}
	grant := replay && f.replayOwed == 0
	if !grant && f.queued+n > f.limit+f.replayOwed {
		f.mu.Unlock()
		f.stop()
		return errSinkBacklog
	}
	if replay && !grant {
		for i := range items {
			items[i].replay = false
		}
	}
	f.queue = append(f.queue, items...)
	f.queued += n
	if grant {
		f.replayOwed += n
	}
	f.mu.Unlock()
	f.signal()
	return nil
}

func (f *frameSink) signal() {
	select {
	case f.wake <- struct{}{}:
	default:
	}
}

// run is the writer goroutine: the only code that writes to the conn
// after the handshake. It exits, closing the conn, on stop, on a write
// error or timeout, or once a graceful Close has drained the queue.
func (f *frameSink) run() {
	defer close(f.done)
	defer f.conn.Close()
	for {
		f.mu.Lock()
		for len(f.queue) == 0 && !f.stopped && !f.closing {
			f.mu.Unlock()
			<-f.wake
			f.mu.Lock()
		}
		if f.stopped || len(f.queue) == 0 {
			f.mu.Unlock()
			return
		}
		batch := f.queue
		f.queue = nil
		f.mu.Unlock()
		for _, it := range batch {
			_ = f.conn.SetWriteDeadline(time.Now().Add(f.timeout))
			if _, err := f.conn.Write(it.b); err != nil {
				f.stop()
				return
			}
			f.mu.Lock()
			f.queued -= len(it.b)
			if it.replay {
				f.replayOwed -= len(it.b)
			}
			stopped := f.stopped
			f.mu.Unlock()
			if stopped {
				return
			}
		}
	}
}

// stop hangs up now: it drops the queue, closes the conn (unblocking a
// writer stuck in Write) and wakes an idle writer so it exits.
// Idempotent. serveAttach defers it on every exit path.
func (f *frameSink) stop() {
	f.mu.Lock()
	f.stopped = true
	f.queue = nil
	f.mu.Unlock()
	_ = f.conn.Close()
	f.signal()
}

// Close is called by Session.fanoutClose when the PTY ends. It is
// graceful: the writer delivers the output already queued (the last
// lines before exit), still bounded by the write timeout, and then
// closes the conn. It must not block: fanoutClose holds Session.mu.
func (f *frameSink) Close() error {
	f.mu.Lock()
	f.closing = true
	f.mu.Unlock()
	f.signal()
	return nil
}
