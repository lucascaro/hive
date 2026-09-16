package agentstate

import (
	"time"

	"github.com/lucascaro/hive/internal/wire"
)

// Agent activity: what the session's agent is doing inside a turn —
// which tool it is running and where it is in its own plan. See
// docs/design-docs/agent-activity.md.
//
// This lives on Machine rather than beside it on registry.Entry so it
// inherits the lifecycle that already exists: New builds a fresh one,
// attachSessionHooks replaces it wholesale on create/restart/revive,
// and deleting the entry frees it. There is no second place to clear,
// which is the whole reason the state machine owns it.
//
// Like the rest of Machine this is deliberately NOT concurrency-safe.
// The registry mutex is the single guard.

const (
	// ActivityRingCap bounds the per-session tool history. It dies with
	// the daemon like the PTY — there is no disk format.
	ActivityRingCap = 200
	// activityOpenCap bounds tool calls awaiting their end. A reporter
	// that dies mid-tool never sends the end, so without a cap this map
	// grows for the life of the session.
	activityOpenCap = 32
)

// openCall is a tool_start waiting for its tool_end.
type openCall struct {
	tool      string
	target    string
	startedAt time.Time
	planIdx   int
}

// activity is the per-session ring plus the latest plan snapshot.
type activity struct {
	ring []wire.ToolEvent
	plan []wire.PlanItem
	open map[string]openCall

	// delta is the tool event the most recent Apply produced, for the
	// ACTIVITY broadcast. It is NOT "the last ring entry": a tool_start
	// that carries a call_id goes into open rather than the ring (it
	// has not finished), so reading the ring would silently broadcast
	// nothing at all when a tool begins — clients would only ever learn
	// that tools had ended.
	delta    wire.ToolEvent
	hasDelta bool
}

// currentPlanIdx is the plan item the agent says it is on, or -1.
func (a *activity) currentPlanIdx() int {
	for i := range a.plan {
		if a.plan[i].Status == wire.PlanStatusActive {
			return i
		}
	}
	return -1
}

// push appends to the ring, evicting the oldest past the cap.
func (a *activity) push(ev wire.ToolEvent) {
	a.ring = append(a.ring, ev)
	if len(a.ring) > ActivityRingCap {
		// Copy down rather than reslice: reslicing would keep the whole
		// backing array alive for the life of the session.
		n := copy(a.ring, a.ring[len(a.ring)-ActivityRingCap:])
		a.ring = a.ring[:n]
	}
}

// evictOldestOpen drops the longest-running unfinished call. A Go map
// has no order, so "oldest" is defined explicitly by startedAt; at 32
// entries the scan is cheaper than maintaining a second index.
func (a *activity) evictOldestOpen() {
	var oldestID string
	var oldestAt time.Time
	for id, c := range a.open {
		if oldestID == "" || c.startedAt.Before(oldestAt) {
			oldestID, oldestAt = id, c.startedAt
		}
	}
	if oldestID != "" {
		delete(a.open, oldestID)
	}
}

// endTurn forgets every call still marked running. A finished turn is
// running nothing, so anything left in open is a call whose end never
// arrived — a hook that was lost, killed or interrupted — and would
// otherwise keep naming a finished tool as CurrentTool indefinitely,
// with only the 32-call cap to ever push it out.
//
// The calls are forgotten, not pushed to the ring: without an end they
// have no outcome and no duration to record, and inventing one would
// put a false "succeeded" in the timeline. Their tally already counted
// at start.
func (a *activity) endTurn() {
	a.open = nil
}

// applyLateActivity handles an event the ordering guard in Apply has
// judged out of order.
//
// For tool events it records the ACTIVITY and nothing else. Parallel
// tools finish in any order and their hooks are separate processes, so
// a tool_end stamped before an already-applied event is ordinary — and
// dropping it whole, as the guard used to, orphaned the call in open
// and pinned CurrentTool to a finished tool. Tool events pair by call
// ID, so their order does not matter to the pairing. What the guard
// exists to protect — the session's state and the staleness clock — is
// left untouched: a late event still must not flip a waiting session
// back to working.
//
// Every other kind stays dropped, plans included: an older plan update
// applied late would regress a step, a completed task back to in
// progress, which order-independent pairing cannot excuse.
func (m *Machine) applyLateActivity(ev Event, now time.Time) bool {
	if m.state == wire.StateExited {
		return false
	}
	before := m.Snapshot()
	switch ev.Kind {
	case KindToolStart:
		m.toolStart(ev, now)
	case KindToolEnd:
		m.toolEnd(ev, now)
	default:
		return false
	}
	return m.Snapshot() != before
}

// toolStart records a tool beginning. now is the DAEMON's clock, not
// the reporter's: durations must not be able to straddle a clock
// adjustment on the reporting side.
func (m *Machine) toolStart(ev Event, now time.Time) {
	idx := m.act.currentPlanIdx()
	// The tally is incremented at START, so a tool that never reports
	// an end still counts against the step that launched it.
	if idx >= 0 {
		m.act.plan[idx].Tools++
	}
	started := wire.ToolEvent{
		Tool:      ev.Tool,
		Target:    ev.Target,
		CallID:    ev.CallID,
		StartedAt: now.UTC().Format(time.RFC3339Nano),
		PlanIdx:   idx,
	}
	// The delta reports the tool as running — OK stays nil, which is
	// exactly what "in flight" means on the wire.
	m.act.delta, m.act.hasDelta = started, true

	if ev.CallID == "" {
		// Nothing to pair with. Record it as an already-closed entry so
		// the timeline still shows that the tool ran.
		m.act.push(started)
		return
	}
	if m.act.open == nil {
		m.act.open = make(map[string]openCall, activityOpenCap)
	}
	if _, exists := m.act.open[ev.CallID]; !exists && len(m.act.open) >= activityOpenCap {
		m.act.evictOldestOpen()
	}
	m.act.open[ev.CallID] = openCall{
		tool:      ev.Tool,
		target:    ev.Target,
		startedAt: now,
		planIdx:   idx,
	}
}

// toolEnd closes a tool call and pushes it to the ring. An end with no
// matching start is recorded unpaired rather than dropped: "this ran
// and we missed the beginning" is more useful than silence.
func (m *Machine) toolEnd(ev Event, now time.Time) {
	out := wire.ToolEvent{
		Tool:    ev.Tool,
		Target:  ev.Target,
		CallID:  ev.CallID,
		EndedAt: now.UTC().Format(time.RFC3339Nano),
		OK:      ev.OK,
		PlanIdx: -1,
	}
	if ev.CallID != "" {
		if open, ok := m.act.open[ev.CallID]; ok {
			delete(m.act.open, ev.CallID)
			out.StartedAt = open.startedAt.UTC().Format(time.RFC3339Nano)
			out.PlanIdx = open.planIdx
			// Both ends are the daemon's own clock, so this subtraction
			// is meaningful in a way reporter timestamps are not.
			if d := now.Sub(open.startedAt); d > 0 {
				out.DurationMS = d.Milliseconds()
			}
			if out.Tool == "" {
				out.Tool = open.tool
			}
			if out.Target == "" {
				out.Target = open.target
			}
		}
	}
	if out.PlanIdx < 0 {
		out.PlanIdx = m.act.currentPlanIdx()
	}
	m.act.push(out)
	m.act.delta, m.act.hasDelta = out, true
}

// setPlan replaces the plan wholesale, carrying each item's tool tally
// forward.
//
// Wholesale replacement is what two reporters give us — a TodoWrite
// call, and the complete list in a TaskList response — but a naive
// replacement would reset every tally each time, since both fire
// repeatedly as the plan evolves.
//
// Tallies are matched by ID where the item has one (the task tools),
// which is exact. Items without one (TodoWrite) fall back to matching
// text, first-match-wins: an agent can emit two steps with identical
// text, and truncation at MaxPlanTextLen can make two long steps
// identical, so each old item is consumed at most once.
func (m *Machine) setPlan(items []wire.PlanItem) {
	old := m.act.plan
	used := make([]bool, len(old))
	next := make([]wire.PlanItem, 0, min(len(items), wire.MaxPlanItems))
	for _, it := range items {
		if len(next) == wire.MaxPlanItems {
			break
		}
		// A deleted step in a full list — never expected, but a
		// TaskList response is agent-authored — is simply absent.
		if it.Status == wire.PlanStatusDeleted {
			continue
		}
		it = normalisePlanItem(it)
		it.Tools = 0
		if i := matchOld(old, used, it); i >= 0 {
			it.Tools = old[i].Tools
			used[i] = true
		}
		next = append(next, it)
	}
	m.act.plan = next
}

// matchOld finds the unconsumed old item it replaces: by ID when it has
// one, else by text. -1 when there is none.
func matchOld(old []wire.PlanItem, used []bool, it wire.PlanItem) int {
	for i := range old {
		if used[i] {
			continue
		}
		if it.ID != "" {
			if old[i].ID == it.ID {
				return i
			}
			continue
		}
		if old[i].ID == "" && old[i].Text == it.Text {
			return i
		}
	}
	return -1
}

// mergePlanItems applies individual step updates by ID — the shape
// Claude's task tools report. Each field present on the update is
// applied and each absent one is left alone: a status-only TaskUpdate
// carries no text, and a rename carries no status, so treating an
// empty field as "clear it" would blank a step on every update.
//
// An ID the plan has never seen is added, not refused. That happens
// when Hive attached after the agent created the task, or the create's
// hook event was lost; the step is still real, and the next TaskList
// resync fills in whatever text it is missing. Items with no ID cannot
// be merged into anything and are ignored.
func (m *Machine) mergePlanItems(items []wire.PlanItem) {
	for _, up := range items {
		if up.ID == "" {
			continue
		}
		idx := -1
		for i := range m.act.plan {
			if m.act.plan[i].ID == up.ID {
				idx = i
				break
			}
		}

		if up.Status == wire.PlanStatusDeleted {
			if idx >= 0 {
				m.act.plan = append(m.act.plan[:idx], m.act.plan[idx+1:]...)
			}
			continue
		}

		if idx < 0 {
			if len(m.act.plan) >= wire.MaxPlanItems {
				continue
			}
			created := normalisePlanItem(wire.PlanItem{ID: up.ID, Text: up.Text, Status: up.Status})
			m.act.plan = append(m.act.plan, created)
			continue
		}

		cur := &m.act.plan[idx]
		if up.Text != "" {
			cur.Text = truncatePlanText(up.Text)
		}
		if wire.PlanStatuses[up.Status] {
			cur.Status = up.Status
		}
		// The tally is never taken from an update: it is the daemon's
		// own count, and a reporter has no business overwriting it.
	}
}

// normalisePlanItem caps the text and coerces an unrecognised status to
// pending — coerced, not dropped: losing a step entirely is worse than
// mislabelling one.
func normalisePlanItem(it wire.PlanItem) wire.PlanItem {
	it.Text = truncatePlanText(it.Text)
	if !wire.PlanStatuses[it.Status] {
		it.Status = wire.PlanStatusPending
	}
	return it
}

// truncatePlanText caps a plan step at the wire limit, on a rune
// boundary — the text is whatever the agent wrote.
func truncatePlanText(s string) string {
	if len(s) <= wire.MaxPlanTextLen {
		return s
	}
	cut := wire.MaxPlanTextLen
	for cut > 0 && !utf8RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// utf8RuneStart reports whether b begins a UTF-8 rune.
func utf8RuneStart(b byte) bool { return b&0xC0 != 0x80 }

// Activity returns the stored ring and the current plan, as copies —
// the caller marshals them onto the wire while the registry lock is
// held elsewhere, and handing out the live slices would let a later
// push alias them.
func (m *Machine) Activity() ([]wire.ToolEvent, []wire.PlanItem) {
	events := make([]wire.ToolEvent, len(m.act.ring))
	copy(events, m.act.ring)
	plan := make([]wire.PlanItem, len(m.act.plan))
	copy(plan, m.act.plan)
	return events, plan
}

// LastToolDelta returns the tool event the most recent Apply produced,
// for the ACTIVITY broadcast, and consumes it. ok is false when no tool
// event has been applied since the last read — including when Apply
// rejected the event (the out-of-order guard), so a dropped event never
// re-broadcasts the previous delta.
//
// A started-but-unfinished tool is reported here with a nil OK and no
// duration; it reaches the ring only when it ends.
func (m *Machine) LastToolDelta() (wire.ToolEvent, bool) {
	if !m.act.hasDelta {
		return wire.ToolEvent{}, false
	}
	m.act.hasDelta = false
	return m.act.delta, true
}

// planSummary is the compact form carried on SessionInfo: how far
// along, and what is running right now. Clients render a row from
// these three values alone and never read the ring.
func (m *Machine) planSummary() (done, total int, current string) {
	for i := range m.act.plan {
		if m.act.plan[i].Status == wire.PlanStatusDone {
			done++
		}
	}
	total = len(m.act.plan)
	// The most recently started unfinished call. Parallel tools mean
	// there can be several; the newest is the one worth naming.
	var newest time.Time
	for _, c := range m.act.open {
		if current == "" || c.startedAt.After(newest) {
			current, newest = c.tool, c.startedAt
		}
	}
	return done, total, current
}
