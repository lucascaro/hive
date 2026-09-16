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
// forward by matching text.
//
// Wholesale replacement is what the reporters give us — a Claude
// TodoWrite call carries the entire list every time — but a naive
// replacement would reset every tally on each fire, since TodoWrite
// fires repeatedly as the plan evolves. Matching by text is
// first-match-wins: an agent can emit two steps with identical text,
// and truncation at MaxPlanTextLen can make two long steps identical,
// so each old item is consumed at most once.
func (m *Machine) setPlan(items []wire.PlanItem) {
	if len(items) > wire.MaxPlanItems {
		items = items[:wire.MaxPlanItems]
	}
	old := m.act.plan
	used := make([]bool, len(old))
	next := make([]wire.PlanItem, 0, len(items))
	for _, it := range items {
		it.Text = truncatePlanText(it.Text)
		if !wire.PlanStatuses[it.Status] {
			// Coerced, not dropped: losing a step entirely is worse
			// than mislabelling one.
			it.Status = wire.PlanStatusPending
		}
		it.Tools = 0
		for i := range old {
			if !used[i] && old[i].Text == it.Text {
				it.Tools = old[i].Tools
				used[i] = true
				break
			}
		}
		next = append(next, it)
	}
	m.act.plan = next
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
// for the ACTIVITY broadcast. ok is false when no tool event has been
// applied yet.
//
// A started-but-unfinished tool is reported here with a nil OK and no
// duration; it reaches the ring only when it ends.
func (m *Machine) LastToolDelta() (wire.ToolEvent, bool) {
	return m.act.delta, m.act.hasDelta
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
