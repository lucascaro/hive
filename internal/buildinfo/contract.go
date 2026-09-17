package buildinfo

// DaemonContract is the compatibility generation of everything the
// daemon exposes to a client: the wire frames it understands, the
// session semantics behind them, and the registry state it persists.
//
// Bump it whenever a GUI built against the new tree cannot correctly
// drive a daemon built against the old one. Do NOT bump it for
// GUI-only changes, or for daemon-side changes a client cannot
// observe (a refactor, a log line, a comment): a needless bump costs
// the user every running session, because the GUI answers a bump by
// demanding a full restart instead of a cheap reload.
//
// This is deliberately NOT BuildID. BuildID is a git revision, so it
// changes for a CSS tweak — comparing it treated every rebuild as a
// stale daemon, which is the bug this constant exists to fix. It is
// also not wire.PROTOCOL_VERSION: that one is a hard gate (the daemon
// refuses a mismatched HELLO outright, see internal/daemon), so it
// cannot express "these two can still talk, but the GUI should
// restart the daemon to pick up new behavior".
//
// scripts/check-daemon-contract.sh fails CI on a PR that touches
// daemon-side code without changing this value.
// History (newest first), so a bump is a decision with a record and
// not just a number going up:
//
//	13 — Activity staleness. ACTIVITY is sent for every accepted
//	    agent event (a bare liveness frame for kinds that carry no
//	    activity), every frame and the GET_ACTIVITY answer carry
//	    stale_at on the daemon clock, and ActivityMsg.plan is no longer
//	    omitempty: null means unchanged, [] means emptied. A GUI built
//	    after this, against an older daemon, never learns a plan was
//	    emptied and renders no staleness in the inspector panel or
//	    activity grid.
//	12 — Subagent attribution. Two new AgentEvent kinds,
//	    subagent_start / subagent_end, from Claude's SubagentStart /
//	    SubagentStop hooks; AgentEvent and ToolEvent gain agent_id /
//	    agent_type, AgentEvent gains running_agents, and SessionInfo
//	    gains subagents_running. Tool events tagged with agent_id no
//	    longer move state, current_tool, the plan or its tally.
//	    Same hazard as 11: an old daemon drops the new kinds at its
//	    allowlist and closes the reporter's connection, so a new hook
//	    against an old daemon loses every event after them on that
//	    connection, and an old daemon still lets a subagent's tool
//	    event flip a finished parent turn back to working.
//	11 — Agent activity. The three hooks that used to collapse into one
//	    permission_resolved now report tool_start / tool_end / plan,
//	    AgentEvent carries tool/target/call_id/ok/items, SessionInfo
//	    carries plan_done/plan_total/current_tool, and two frames are
//	    new: GET_ACTIVITY (0x2b) and ACTIVITY (0x2c).
//	    The bump is NOT merely "the new GUI shows no activity against
//	    an old daemon". An old daemon drops any AGENT_EVENT whose kind
//	    is outside its allowlist (internal/daemon: "unknown kind"), so
//	    a new `hived hook` reporting tool_start/tool_end to one loses
//	    the working-state effect permission_resolved used to deliver —
//	    a session mid-tool reads as idle or stays stuck on its last
//	    wait. That is a STATE regression, the same class as 2, not just
//	    a missing feature. The hook binary and the daemon ship in one
//	    bundle, so they only disagree across an in-place upgrade, which
//	    is exactly what this constant is read to prevent.
//	10 — Finished and failed agent turns want the user. turn_end now
//	    lands in waiting_input, an agent-reported error stands until the
//	    user looks and counts toward needs_attention, and the new `idle`
//	    event kind is a plain stop. No frame or field changed; like 8 the
//	    change lives entirely in the daemon (and the Pi extension it
//	    writes at start), so without a bump the GUI offers its cheap
//	    reload and the user keeps a hived that still drops finished turns
//	    to idle and lets errors fade.
//	9 — Worktree group names. SET_WORKTREE_LABEL (0x2a) sets or clears
//	    the user-authored name of a worktree group, persisted on the
//	    owning project (ProjectMetaFile.worktree_labels) and delivered
//	    on ProjectInfo.worktree_labels, broadcast as PROJECT_EVENT so
//	    every open sidebar repaints rather than only the connection
//	    that set it. An old daemon does not know the frame and would
//	    drop it on the floor, so a new GUI would show the name it just
//	    typed, report success, and persist nothing — the silent
//	    wrong-answer mode this constant exists to prevent. Renames no
//	    session, branch or directory, and is the one worktree mutation
//	    allowed while sessions live in the worktree.
//	8 — DEC private modes survive a reattach. The snapshot and the
//	    resize replay now end by re-asserting whatever the program had
//	    set — bracketed paste (2004), mouse tracking (9/1000/1002/1003),
//	    mouse encoding (1005/1006/1015), app-cursor keys (1) and focus
//	    reporting (1004). The snapshot's DECSTR had been clearing all of
//	    them while only alt-screen and cursor visibility were restored,
//	    so a reattached tile lost them for the life of the session. No
//	    frame or field changed, and that is exactly why it needs the
//	    bump: the fix lives entirely in the daemon, so without one the
//	    GUI offers its cheap reload, the user keeps the hived they
//	    already had, and a >1 KiB paste still arrives split in three
//	    while they believe they have the fix. The bump forces the
//	    daemon restart that actually delivers it.
//	7 — Starting a session from an idea. CreateSpec gained
//	    initial_prompt (delivered as an argv positional to Claude and
//	    Pi, typed into the PTY on the first idle edge for the agents
//	    that present a prompt box, and refused outright for the shell
//	    and custom agents) and idea_id (the daemon flips that idea to `started`
//	    once the prompt lands). UPDATE_IDEA gained kind and project_id,
//	    so the inbox can correct a mis-filed note. Both directions are
//	    silent-wrong without the bump, which is why it is one: an older
//	    daemon drops the unknown JSON fields, so a newer GUI would show
//	    a session that never got its prompt and a re-kind that never
//	    happened — a wrong answer, not an empty one.
//	6 — Narrowed socket for agent children. The daemon binds a second
//	    listener next to the control socket (<sock>.events) serving only
//	    HELLO{mode:event} (one state report) and the new
//	    HELLO{mode:session} (ADD_IDEA / LIST_IDEAS, and a SESSIONS
//	    snapshot narrowed to the caller's own session); every other mode
//	    and verb is answered with mode_not_allowed. HIVE_SOCKET in a
//	    spawned session's environment now names THAT socket, and
//	    `hive idea` speaks mode:session rather than mode:control. A
//	    session revived by an older daemon hands its agent the control
//	    socket, and an older `hive idea` dialling the events socket in
//	    mode:control is refused, so the pair has to move together —
//	    which is exactly what a bump forces.
//	5 — Ideas: the LIST_IDEAS / IDEAS / ADD_IDEA / UPDATE_IDEA /
//	    REMOVE_IDEA / IDEA_EVENT frame set, the registry-owned ideas/
//	    directory behind it, and the project_has_ideas refusal that
//	    KILL_PROJECT now answers with when a delete would destroy open
//	    ideas. A GUI built after this against an older daemon gets an
//	    inbox that never resolves; a daemon built after it refuses a
//	    project delete an older GUI cannot confirm.
//	4 — ModeEvent + FrameAgentEvent: a new connection mode an agent's
//	    hook (`hived hook`) or extension dials to report a state
//	    observation. A GUI never opens this mode itself, but a daemon
//	    that predates it answers HELLO{mode:event} with unknown_mode,
//	    which is why the bump: an old daemon paired with a new hook is
//	    silently missing the hook tier rather than erroring loudly.
//	3 — SessionInfo gained state, state_source, last_prompt and
//	    last_summary, plus the SESSION_EVENT(state) kind that reports
//	    them. A GUI built before this shows no state glyphs at all; a
//	    GUI built after it, talking to an older daemon, would show
//	    every session as permanently idle.
//	2 — SessionInfo gained needs_attention, driven by a daemon-side
//	    bell scanner and cleared through UPDATE_SESSION. A GUI built
//	    before this cannot see or clear the flag.
//	1 — first contract; everything up to and including the
//	    CLIENT_COMMAND relay.
const DaemonContract = 13

// Identity is this binary's full build identity. `hived --version
// --json` prints it, and Welcome carries the same three values, so a
// GUI can interrogate BOTH the daemon it is talking to and a staged
// update bundle it has not installed yet — and decide between a
// GUI-only reload and a full restart before the user commits to
// either.
type Identity struct {
	Release        string `json:"release"`
	BuildID        string `json:"build_id"`
	DaemonContract int    `json:"daemon_contract"`
}

// CurrentIdentity returns this binary's Identity.
func CurrentIdentity() Identity {
	return Identity{
		Release:        Version(),
		BuildID:        BuildID(),
		DaemonContract: DaemonContract,
	}
}
