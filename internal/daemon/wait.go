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
// Events are only wake-ups. The registry builds many of them from an
// Info() taken before it released its lock and publishes them after, and
// some state changes ride an "updated" event, so what an event says about
// state can be older than what the registry already knows. The wait
// therefore subscribes FIRST (a change after a look is then a wake-up it
// holds, never lost) and decides from a fresh Registry.StateView each
// time: the live state, plus the ordered steps the session took since the
// last look. The steps cover what a snapshot cannot show — a wanted state
// reached and left between two looks, an exit followed by a revive, or a
// stretch while the registry had dropped a slow listener — and being in
// order they make the FIRST wanted state the one reported.
func (d *Daemon) awaitState(ctx context.Context, id string, want map[string]bool, afterWorking bool, timeout time.Duration) (string, error) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	seq, first := registry.StateSeqNone, true
	sawWorking := !afterWorking
	retries := 0
	for {
		listener, unsub := d.reg.Subscribe()
		for {
			info, steps, now, lost, found := d.reg.StateView(id, seq)
			if !found {
				// A registry that is closing empties itself and hands out
				// closed listeners; that is shutdown, not the target going.
				closed := false
				select {
				case _, open := <-listener:
					closed = !open
				default:
				}
				unsub()
				if closed {
					return "", errors.New("daemon shutting down")
				}
				return wire.WaitStateGone, nil
			}
			seq = now
			// An idle that predates the agent picking up its work does not
			// count under afterWorking; leaving idle does. The first look
			// has no steps, only the live state.
			if first && info.State != wire.StateIdle {
				sawWorking = true
			}
			first = false
			if lost {
				steps = nil
			}
			if st, ok := firstWanted(append(steps, info.State), want, &sawWorking); ok {
				unsub()
				return st, nil
			}
			st, wake := d.nextWake(ctx, listener, timer, id)
			if wake == wakeEvent {
				retries = 0
				continue
			}
			unsub()
			switch wake {
			case wakeTimeout:
				return st, nil
			case wakeCtx:
				return "", errors.New("daemon shutting down")
			}
			// wakeClosed: dropped (slow reader) or closing. Resubscribe and
			// look again; a registry that keeps handing back closed
			// listeners is closing.
			if retries++; retries > waitResubscribeLimit {
				return "", errors.New("daemon shutting down")
			}
			break
		}
	}
}

// firstWanted walks states in order and returns the first wanted one.
// *sawWorking is updated as it goes: leaving idle at any step is the
// work an afterWorking wait is for, and an idle before it does not count.
func firstWanted(states []string, want map[string]bool, sawWorking *bool) (string, bool) {
	for _, st := range states {
		if st != wire.StateIdle {
			*sawWorking = true
		}
		if want[st] && (st != wire.StateIdle || *sawWorking) {
			return wire.StateName(st), true
		}
	}
	return "", false
}

type wakeKind int

const (
	wakeEvent   wakeKind = iota // an event for the target arrived
	wakeClosed                  // the listener was closed
	wakeTimeout                 // the timeout elapsed
	wakeCtx                     // the daemon is shutting down
)

// nextWake blocks for the next event about id (any kind: the caller looks
// at the registry, not at the event), the timeout, or the listener
// closing. The state is wire.WaitStateTimeout when the timer fired.
func (d *Daemon) nextWake(ctx context.Context, listener registry.Listener, timer *time.Timer, id string) (string, wakeKind) {
	for {
		select {
		case ev, ok := <-listener:
			if !ok {
				return "", wakeClosed
			}
			if ev.Session.ID != id {
				continue
			}
			if waitEventHook != nil {
				waitEventHook()
			}
			return "", wakeEvent
		case <-timer.C:
			return wire.WaitStateTimeout, wakeTimeout
		case <-ctx.Done():
			return "", wakeCtx
		}
	}
}
