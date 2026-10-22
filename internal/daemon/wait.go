package daemon

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/lucascaro/hive/internal/registry"
	"github.com/lucascaro/hive/internal/wire"
)

// waitResubscribeLimit bounds consecutive re-subscribes that delivered
// nothing. A listener the registry dropped for being slow is replaced
// and the wait carries on; one that closes again at once means the
// registry itself is closing.
const waitResubscribeLimit = 5

// defaultWaitStates is what WAIT_SESSION means by "done": the session
// is at rest or gone, as opposed to merely between turns.
var defaultWaitStates = []string{"idle", wire.StateExited, wire.StateError}

// waitEventHook, when set, runs after drainWait takes an event for the
// target. Var, not inlined, so a test can hold the consumer still while
// events pile up and the registry drops its listener.
var waitEventHook func()

// waitSession serves WAIT_SESSION for a ModeSession connection: block
// until the target — a sibling in the caller's project — reaches one of
// the requested states, then answer WAITED (spec 390).
//
// It runs inline on the connection's read loop. Nothing reads the
// socket while it waits, so the 30 s session-mode idle deadline cannot
// fire; the timeout (capped at wire.WaitMaxTimeout) is what stops a
// client that hung up from parking this goroutine for good.
//
// Read-only and grantless: a same-user process can already read every
// state it reports from the control socket.
func (d *Daemon) waitSession(ctx context.Context, ops controlOps, req wire.WaitSessionReq) {
	target, err := d.reg.ResolveSibling(ops.ownSessionID, req.SessionID)
	if err != nil {
		if errors.Is(err, registry.ErrSelfMessage) {
			ops.sendError("bad_payload", "a session cannot wait on itself")
			return
		}
		ops.sendError(sendErrCode(err), sendErrMessage(err))
		return
	}
	want, err := parseWaitStates(req.States)
	if err != nil {
		ops.sendError("bad_payload", err.Error())
		return
	}
	// Compare in seconds before converting: a huge raw value would
	// overflow time.Duration into a negative, already-expired timer.
	var timeout time.Duration
	switch {
	case req.TimeoutSeconds < 0:
		ops.sendError("bad_payload", "timeout_s must not be negative")
		return
	case req.TimeoutSeconds == 0:
		timeout = wire.WaitDefaultTimeout
	case req.TimeoutSeconds > int(wire.WaitMaxTimeout/time.Second):
		timeout = wire.WaitMaxTimeout
	default:
		timeout = time.Duration(req.TimeoutSeconds) * time.Second
	}

	reached, err := d.awaitState(ctx, target.ID, want, req.AfterWorking, timeout)
	if err != nil {
		log.Printf("daemon: wait caller=session from=%s to=%s reached=error", ops.ownSessionID, target.ID)
		ops.sendError("delivery_failed", err.Error())
		return
	}
	if err := ops.writeJSON(wire.FrameWaited, wire.WaitedResp{SessionID: target.ID, State: reached}); err != nil {
		reached = "disconnect"
	}
	// One greppable line per wait: the evidence that gates the
	// orchestration phases (agent-orchestration.md).
	log.Printf("daemon: wait caller=session from=%s to=%s reached=%s", ops.ownSessionID, target.ID, reached)
}

// parseWaitStates turns the wire names into wire state values.
func parseWaitStates(names []string) (map[string]bool, error) {
	if len(names) == 0 {
		names = defaultWaitStates
	}
	want := make(map[string]bool, len(names))
	for _, n := range names {
		st, ok := wire.ParseStateName(n)
		if !ok {
			return nil, fmt.Errorf("unknown state %q; want one of %v", n, wire.WaitStateNames)
		}
		want[st] = true
	}
	return want, nil
}

// awaitState blocks until id is in a wanted state and returns its wire
// name, or wire.WaitStateTimeout / wire.WaitStateGone.
//
// Order matters: subscribe BEFORE reading the current state, so a
// transition between the two is delivered rather than lost. A listener
// the registry drops (slow reader) is replaced and the state re-read,
// so a dropped event costs nothing either.
func (d *Daemon) awaitState(ctx context.Context, id string, want map[string]bool, afterWorking bool, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	sawWorking := !afterWorking
	// settle reports whether info ends the wait. With afterWorking an
	// idle that predates the agent picking up its work does not count;
	// exited and error are terminal and always do.
	settle := func(info wire.SessionInfo) (string, bool) {
		if info.State != wire.StateIdle {
			sawWorking = true
		}
		if !want[info.State] || (info.State == wire.StateIdle && !sawWorking) {
			return "", false
		}
		return wire.StateName(info.State), true
	}

	retries := 0
	for {
		listener, unsub := d.reg.Subscribe()
		info, found := d.sessionInfo(id)
		if !found {
			// A registry that is closing empties itself and hands out
			// closed listeners; that is shutdown, not the target going.
			select {
			case _, open := <-listener:
				unsub()
				if !open {
					return "", errors.New("daemon shutting down")
				}
			default:
				unsub()
			}
			return wire.WaitStateGone, nil
		}
		if st, ok := settle(info); ok {
			unsub()
			return st, nil
		}
		st, done, delivered := d.drainWait(ctx, listener, timer, id, want, settle)
		unsub()
		if delivered {
			retries = 0
		}
		if done {
			return st, nil
		}
		if ctx.Err() != nil {
			return "", errors.New("daemon shutting down")
		}
		if retries++; retries > waitResubscribeLimit {
			return "", errors.New("daemon shutting down")
		}
	}
}

// drainWait consumes events for id until the wait ends (done) or the
// listener closes (done false: the caller re-subscribes). delivered
// reports whether any event arrived on this listener.
func (d *Daemon) drainWait(ctx context.Context, listener registry.Listener, timer *time.Timer, id string, want map[string]bool,
	settle func(wire.SessionInfo) (string, bool)) (state string, done, delivered bool) {
	for {
		select {
		case ev, ok := <-listener:
			if !ok {
				return "", false, delivered
			}
			if ev.Session.ID != id {
				continue
			}
			delivered = true
			if waitEventHook != nil {
				waitEventHook()
			}
			if ev.Kind == wire.SessionEventRemoved {
				return wire.WaitStateGone, true, true
			}
			if st, ok := settle(ev.Session); ok {
				// The event may predate the snapshot awaitState took
				// (an idle queued before a later transition to working).
				// Trust it only if the session is still in a wanted state.
				if cur, found := d.sessionInfo(id); found && !want[cur.State] {
					continue
				}
				return st, true, true
			}
		case <-timer.C:
			return wire.WaitStateTimeout, true, delivered
		case <-ctx.Done():
			return "", false, delivered
		}
	}
}

// sessionInfo is the registry's current view of one session.
func (d *Daemon) sessionInfo(id string) (wire.SessionInfo, bool) {
	for _, s := range d.reg.List() {
		if s.ID == id {
			return s, true
		}
	}
	return wire.SessionInfo{}, false
}
