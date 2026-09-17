// hive.ts is the Hive extension-tier reporter for Pi. hived writes this
// file to <stateDir>/pi/hive.ts at daemon start (see
// internal/agent/pi.go) and every Pi session Hive spawns is launched
// with `pi -e <that path>`, so Pi reports what it is doing instead of
// leaving the session on the PTY heuristic tier.
//
// Outside Hive the extension is inert: without HIVE_SESSION_ID and
// HIVE_SOCKET in the environment it subscribes to nothing and returns.
//
// Wire format (keep in sync with internal/wire/frame.go — this is the
// only encoder of Hive frames outside Go):
//
//   +-------+--------------+---------+
//   | type  | len (BE u32) | payload |
//   | 1 B   | 4 B          | len B   |
//   +-------+--------------+---------+
//
// A report is one connection: HELLO{mode:"event"} then one or more
// AGENT_EVENTs (a successful hive_todo call is a tool_end AND a plan),
// then close. The daemon replies to neither, so nothing here ever reads
// from the socket.
//
// Privacy rule (docs/design-docs/agent-activity.md): a tool's raw
// arguments and result never cross the socket. Only the tool name, a
// derived label (deriveTarget) and the plan items do. eventBody builds
// every frame from an explicit field list for exactly that reason —
// nothing here spreads a Pi event into a payload.
import net from "node:net";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const FRAME_HELLO = 0x01;
const FRAME_AGENT_EVENT = 0x22;
const PROTOCOL_VERSION = 1;

// Caps mirroring internal/wire (control.go). The daemon truncates again
// on receipt; capping here is what keeps a pasted-file-sized prompt
// under wire.MaxPayload, where an oversized frame would be refused whole
// and lose the kind along with the text.
const MAX_SUMMARY_LEN = 512; // wire.MaxSummaryLen
const MAX_TARGET_LEN = 120; // wire.MaxTargetLen
const MAX_ID_LEN = 128; // wire.MaxToolNameLen / wire.MaxActivityIDLen
const MAX_PLAN_TEXT_LEN = 200; // wire.MaxPlanTextLen
const MAX_PLAN_ITEMS = 100; // wire.MaxPlanItems

// The daemon reads at most this many AGENT_EVENTs per connection
// (eventMaxFrames in internal/daemon/daemon.go) and drops the rest.
export const MAX_EVENTS_PER_REPORT = 8;

// Reports waiting behind an in-flight connection. See createSender.
export const MAX_PENDING_REPORTS = 64;

// The tool Hive adds to Pi so a session has a plan to show. Not "todo":
// Pi's own examples/extensions/todo.ts registers that name, and a user
// loading both would get a clash.
export const TODO_TOOL_NAME = "hive_todo";

// Set to "0" by hived (internal/agent/settings.go, piSpawnEnv) when the
// user turned the tool off in Settings → Agents.
export const TODO_TOOL_ENV = "HIVE_PI_TODO_TOOL";

const PLAN_STATUSES = new Set(["pending", "active", "done"]);

export type PlanItem = { text: string; status: string };

// ReportEvent is one AGENT_EVENT's Hive-side content. `kind` must be on
// wire.AgentEventKinds; internal/agent/pi_test.go scrapes the literals.
export type ReportEvent = {
  kind: string;
  text?: string;
  tool?: string;
  target?: string;
  call_id?: string;
  ok?: boolean;
  items?: PlanItem[];
};

function frame(type: number, payload: unknown): Buffer {
  const body = Buffer.from(JSON.stringify(payload), "utf8");
  const head = Buffer.alloc(5);
  head.writeUInt8(type, 0);
  head.writeUInt32BE(body.length, 1);
  return Buffer.concat([head, body]);
}

// eventBody is the AGENT_EVENT payload for one event, every field
// capped. Explicit fields only — see the privacy rule above.
function eventBody(sessionId: string, e: ReportEvent, at: string) {
  return {
    session_id: sessionId,
    kind: e.kind,
    source: "extension",
    ...(e.text ? { text: truncateBytes(e.text, MAX_SUMMARY_LEN) } : {}),
    ...(e.tool ? { tool: truncateBytes(e.tool, MAX_ID_LEN) } : {}),
    ...(e.target ? { target: truncateBytes(e.target, MAX_TARGET_LEN) } : {}),
    ...(e.call_id ? { call_id: truncateBytes(e.call_id, MAX_ID_LEN) } : {}),
    ...(typeof e.ok === "boolean" ? { ok: e.ok } : {}),
    ...(Array.isArray(e.items) ? { items: capPlan(e.items) } : {}),
    at,
  };
}

// encodeFrames builds one report: HELLO plus an AGENT_EVENT per event,
// all stamped with the same `at`. Exported so a Go test can decode what
// this encoder actually produces rather than trusting a hand-written
// fixture to stay in sync.
export function encodeFrames(sessionId: string, events: ReportEvent[], at: string): Buffer {
  const hello = frame(FRAME_HELLO, {
    version: PROTOCOL_VERSION,
    client: "hive-pi-ext",
    mode: "event",
  });
  return Buffer.concat([hello, ...events.map((e) => frame(FRAME_AGENT_EVENT, eventBody(sessionId, e, at)))]);
}

// truncateBytes cuts to max *bytes* (the Go side's unit), backing off
// any partial UTF-8 sequence the cut created.
//
// The cut walks back over continuation bytes (10xxxxxx) rather than
// decoding and stripping a trailing replacement char: a U+FFFD the user
// actually typed is real text, and dropping it would make this differ
// from the Go side's strings.ToValidUTF8, which only rewrites bytes
// that are genuinely invalid.
export function truncateBytes(s: string, max: number): string {
  const b = Buffer.from(s, "utf8");
  if (b.length <= max) return s;
  let end = max;
  while (end > 0 && (b[end] & 0xc0) === 0x80) end--;
  return b.subarray(0, end).toString("utf8");
}

// truncate is truncateBytes at wire.MaxSummaryLen.
export function truncate(s: string): string {
  return truncateBytes(s, MAX_SUMMARY_LEN);
}

function capPlan(items: PlanItem[]): PlanItem[] {
  return items
    .slice(0, MAX_PLAN_ITEMS)
    .map((it) => ({ text: truncateBytes(it.text, MAX_PLAN_TEXT_LEN), status: it.status }));
}

// createSender serializes reports over the socket: the next connection
// is dialed only once the previous one has closed.
//
// Without that, every report raced its own dial, and the daemon's
// ordering guard (internal/agentstate/activity.go, applyLateActivity)
// silently drops a wholesale plan that lands after a later-stamped
// event — so a parallel tool's tool_end could erase the final "all done"
// plan. The daemon applies a connection's frames before closing it, so
// waiting for close makes delivery order the stamp order.
//
// Fire-and-forget still holds. Every way a connection ends — a clean
// close, a refused dial, the idle timeout on a wedged daemon, a
// synchronous throw — advances the queue. The queue is bounded, and a
// full one drops its OLDEST report: behind a wedged daemon the backlog
// is stale anyway (64 x a 2 s timeout outlives agentstate.HookStaleAfter),
// and the newest report is the one that describes the session now.
export function createSender(sock: string, sid: string, timeoutMs = 2000) {
  const queue: Buffer[] = [];
  let busy = false;

  const pump = () => {
    if (busy) return;
    const buf = queue.shift();
    if (!buf) return;
    busy = true;
    let advanced = false;
    const next = () => {
      if (advanced) return;
      advanced = true;
      busy = false;
      pump();
    };
    try {
      const conn = net.createConnection(sock);
      conn.on("error", () => {}); // "close" follows
      conn.on("close", next);
      conn.setTimeout(timeoutMs, () => conn.destroy());
      conn.on("connect", () => {
        conn.end(buf);
      });
    } catch {
      next();
    }
  };

  return {
    // send reports events as one connection. `at` is stamped here, not
    // at dial time: it is when Pi observed this (wire.AgentEvent's
    // documented contract), and the daemon orders events by it.
    send(events: ReportEvent[]): boolean {
      if (events.length === 0 || events.length > MAX_EVENTS_PER_REPORT) return false;
      queue.push(encodeFrames(sid, events, new Date().toISOString()));
      if (queue.length > MAX_PENDING_REPORTS) queue.shift();
      pump();
      return true;
    },
    // pending is the number of reports not yet dialed. For tests.
    pending: () => queue.length,
  };
}

// planFromTodos turns hive_todo's todos (its parameters, or the details
// its result carries) into plan items, or undefined when the shape is
// not a todo list. An unrecognised status reads as pending, as the
// daemon's normalisePlanItem would.
export function planFromTodos(value: any): PlanItem[] | undefined {
  const todos = value?.todos;
  if (!Array.isArray(todos)) return undefined;
  const items: PlanItem[] = [];
  for (const t of todos) {
    if (typeof t?.text !== "string" || t.text === "") continue;
    items.push({ text: t.text, status: PLAN_STATUSES.has(t.status) ? t.status : "pending" });
  }
  return items;
}

// planFromBranch rebuilds the plan from the session itself: the todos
// of the last successful hive_todo result on the current branch, or an
// empty plan when there is none.
//
// The state lives in the tool result's details, as Pi's own todo example
// keeps it, so a resume, a fork or a /tree jump shows the plan of the
// branch the user is now on. An errored result is skipped even when it
// carries details — an afterToolCall hook can flip isError and keep
// them — because the live tool_execution_end handler posted no plan for
// that call either.
export function planFromBranch(ctx: any): PlanItem[] {
  try {
    const entries = ctx?.sessionManager?.getBranch?.() ?? [];
    for (let i = entries.length - 1; i >= 0; i--) {
      const msg = entries[i]?.message;
      if (msg?.role !== "toolResult" || msg.toolName !== TODO_TOOL_NAME || msg.isError === true) continue;
      const items = planFromTodos(msg.details);
      if (items) return items;
    }
  } catch {
    // ignore
  }
  return [];
}

// The todo tool's parameters as a plain JSON Schema. Pi validates tool
// arguments with TypeBox's compiler, which takes JSON Schema; importing
// TypeBox's builders instead would break loading this file anywhere but
// inside Pi (node --test has no node_modules to resolve it from).
const TODO_PARAMETERS = {
  type: "object",
  properties: {
    todos: {
      type: "array",
      maxItems: MAX_PLAN_ITEMS,
      description: "The whole plan, in order. Send every item on every call.",
      items: {
        type: "object",
        properties: {
          text: { type: "string", description: "One step, in a few words." },
          status: { type: "string", enum: ["pending", "active", "done"] },
        },
        required: ["text", "status"],
      },
    },
  },
  required: ["todos"],
};

const STATUS_MARK: Record<string, string> = { pending: "[ ]", active: "[>]", done: "[x]" };

export default function (pi: ExtensionAPI) {
  const sid = process.env.HIVE_SESSION_ID;
  const sock = process.env.HIVE_SOCKET;
  if (!sid || !sock) return; // not under Hive: inert

  const todoTool = process.env[TODO_TOOL_ENV] !== "0";
  const sender = createSender(sock, sid);

  // post reports a single event. Pass the kind as a string literal:
  // TestPiExtensionKindsAreOnTheAllowlist scrapes post("…") calls and
  // `kind: "…"` inside send([...]) calls, and a kind held in a variable
  // is invisible to it.
  const post = (kind: string, text = "", fields: Omit<ReportEvent, "kind" | "text"> = {}) => {
    sender.send([{ ...fields, kind, text }]);
  };

  if (todoTool) {
    pi.registerTool({
      name: TODO_TOOL_NAME,
      label: "Plan",
      description:
        "Track your plan for multi-step work so the user can follow your progress. " +
        "Call it with the whole list every time: each step's text and status " +
        "(pending, active or done). Keep exactly one step active while you work on it.",
      parameters: TODO_PARAMETERS as any,
      async execute(_toolCallId: string, params: any) {
        const todos = planFromTodos(params) ?? [];
        const done = todos.filter((t) => t.status === "done").length;
        const lines = todos.map((t) => `${STATUS_MARK[t.status]} ${t.text}`);
        return {
          content: [{ type: "text", text: [`Plan: ${done}/${todos.length} done.`, ...lines].join("\n") }],
          details: { todos },
        };
      },
    } as any);
  }

  pi.on("session_start", (_event, ctx) => {
    // Every start — startup, reload, /new, /resume, fork — reports the
    // plan of the branch it lands on, empty when there is none, so a
    // new conversation never shows the previous one's plan.
    if (todoTool) sender.send([{ kind: "ping" }, { kind: "plan", items: planFromBranch(ctx) }]);
    else post("ping");
  });

  // A /tree jump moves to another branch without a session_start.
  pi.on("session_tree", (_event, ctx) => {
    if (todoTool) sender.send([{ kind: "plan", items: planFromBranch(ctx) }]);
  });

  // The non-blocking pair. Pi's tool_call / tool_result handlers can
  // mutate or veto the call, and a wedged report there would stall the
  // agent's own tool execution; an observer stays out of that path.
  pi.on("tool_execution_start", (event) => {
    post("tool_start", "", {
      tool: str(event?.toolName),
      target: deriveTarget(event?.args),
      call_id: str(event?.toolCallId),
    });
  });

  // No target: the daemon keeps the start's. ok is always explicit —
  // false is an answer, not an absence. A successful hive_todo call
  // reports its plan on the same connection, read back from the result
  // it produced; a failed one changes no plan.
  pi.on("tool_execution_end", (event) => {
    const end = { tool: str(event?.toolName), call_id: str(event?.toolCallId), ok: event?.isError !== true };
    const plan =
      todoTool && event?.toolName === TODO_TOOL_NAME && event?.isError !== true
        ? planFromTodos(event?.result?.details)
        : undefined;
    if (plan) sender.send([{ kind: "tool_end", ...end }, { kind: "plan", items: plan }]);
    else sender.send([{ kind: "tool_end", ...end }]);
  });

  pi.on("input", (event) => {
    // "extension" input is a message another extension injected, not
    // the user typing; reporting it as a prompt would show the session
    // as working on something nobody asked for.
    if (event.source !== "extension") post("prompt", event.text);
  });

  // Whether Pi is mid-run, which decides what the end of a UI prompt
  // means. Tracked here rather than read off ctx.isIdle() because the
  // prompt ui_prompt_end closes may itself have been raised from inside
  // a turn, and only this extension knows which.
  let turnInFlight = false;

  // How the latest run ended. Pi reports a failed or interrupted run as
  // an assistant message's stopReason, not as an event of its own.
  // agent_end can be retried, so it only records the latest attempt;
  // agent_settled reports it once nothing else will run.
  let ending = runEnd(undefined);

  pi.on("agent_start", () => {
    turnInFlight = true;
    ending = runEnd(undefined);
    post("permission_resolved");
  });

  pi.on("agent_end", (event) => {
    ending = runEnd(event?.messages);
  });

  // agent_end can be followed by an auto-retry or a queued follow-up;
  // agent_settled is the one that means Pi has stopped. It fires for an
  // Esc abort too (from a finally), which is the user acting in this
  // session — idle, not a finished turn calling them back.
  pi.on("agent_settled", (_event, ctx) => {
    turnInFlight = false;
    if (ending.kind === "error") post("error", ending.text);
    else if (ending.kind === "aborted") post("idle", lastAssistantText(ctx));
    else post("turn_end", lastAssistantText(ctx));
    ending = runEnd(undefined);
  });

  // Pi has no built-in permission prompt the way Claude does — a
  // permission gate is an extension calling ctx.ui.confirm(). These
  // two events fire around every blocking extension UI prompt, which
  // is exactly "the session is waiting for the user".
  pi.on("ui_prompt_start", (event) => {
    const kind = event?.kind;
    post(kind === "confirm" || kind === "select" ? "waiting_permission" : "waiting_input");
  });

  // What ends a wait depends on what Pi goes back to. Mid-turn the
  // answer resumes the run, so permission_resolved (which the machine
  // reads as working) is right. Outside a turn — an extension slash
  // command, a confirm() raised after agent_settled — nothing is going
  // to run, and reporting "working" would strand the session there
  // until the tier goes stale 30 s later (agentstate.HookStaleAfter),
  // since only PTY output can demote it. idle, not turn_end: the user
  // just answered, so there is nothing to call them back for.
  pi.on("ui_prompt_end", (_event, ctx) => {
    if (turnInFlight) post("permission_resolved");
    else post("idle", lastAssistantText(ctx));
  });

  // Only "quit" ends the pi process. "new", "resume", "fork" and
  // "reload" tear down the session runtime *inside a live pi* and
  // immediately stand another one up, so reporting session_end for them
  // would be a lie with no way back: StateExited is terminal in
  // agentstate.Machine.Apply, which drops every later event, and the
  // PTY is still very much alive.
  //
  // The replacement path reports idle rather than nothing, because the
  // command that triggered it (`/new`) arrives as an `input` event first
  // and has already moved the session to working. Posting nothing would
  // strand it there until the tier goes stale; posting turn_end would
  // call the user back to a session they just typed into. idle with no
  // text also clears lastSummary, which is right: the previous
  // conversation's closing line does not describe the new one.
  //
  // ponytail: lastPrompt survives the swap and will still show the old
  // session's first prompt — Apply only ever sets it once and no wire
  // kind resets it. Fixing that needs a session-reset event kind, which
  // is a wire change; revisit if the stale prompt is confusing in
  // practice.
  pi.on("session_shutdown", (event) => {
    turnInFlight = false;
    if (event?.reason === "quit") post("session_end");
    else post("idle");
  });
}

function str(v: unknown): string {
  return typeof v === "string" ? v : "";
}

// --- Tool labels ---
//
// A port of cmd/hived/toollabel.go, the Claude reporter's half of the
// privacy rule; read that file for why each rule exists. The two must
// agree, and testdata/toollabel_vectors.json (beside this file) is the table
// both test suites run to keep them agreeing.

// An allowlist, in order: a tool whose arguments are not recognised
// gets no label at all rather than a stringification of them.
const LABEL_KEYS: Array<[string, (v: string) => string]> = [
  ["command", commandHead],
  ["file_path", baseName],
  ["path", baseName],
  ["notebook_path", baseName],
  ["url", urlHost],
];

// deriveTarget produces the label for one tool call's arguments, or ""
// when nothing in them is on the allowlist.
export function deriveTarget(input: unknown): string {
  try {
    if (input === null || typeof input !== "object" || Array.isArray(input)) return "";
    for (const [key, derive] of LABEL_KEYS) {
      const v = (input as Record<string, unknown>)[key];
      if (typeof v !== "string" || v === "") continue;
      const out = derive(v);
      if (out) return truncateBytes(out, MAX_TARGET_LEN);
    }
  } catch {
    // ignore
  }
  return "";
}

// What Go's strings.Fields splits on (unicode.IsSpace). JS's \s differs:
// it adds U+FEFF and lacks U+0085.
const GO_SPACE = /[\t\n\v\f\r \u0085\u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000]+/;

function commandHead(cmd: string): string {
  const meta = cmd.search(/[;|&\n\r<>()]/);
  if (meta >= 0) cmd = cmd.slice(0, meta);
  const fields = cmd.split(GO_SPACE).filter((f) => f !== "");
  while (fields.length > 0 && isEnvAssignment(fields[0])) fields.shift();
  if (fields.length === 0) return "";
  let head = baseName(fields[0]);
  if (!isCommandName(head)) return "";
  if (fields.length > 1 && isSubcommand(fields[1])) head += " " + fields[1];
  return head;
}

function isEnvAssignment(tok: string): boolean {
  const eq = tok.indexOf("=");
  if (eq <= 0) return false;
  for (let i = 0; i < eq; i++) {
    const c = tok.charCodeAt(i);
    const letter = c === 0x5f || (c >= 0x41 && c <= 0x5a) || (c >= 0x61 && c <= 0x7a);
    if (!letter && (i === 0 || c < 0x30 || c > 0x39)) return false;
  }
  return true;
}

function isCommandName(tok: string): boolean {
  return tok !== "" && !/[=$"'@:`]/.test(tok);
}

const MAX_SUBCOMMAND_LEN = 20;

function isSubcommand(tok: string): boolean {
  if (tok === "" || tok.startsWith("-") || Buffer.byteLength(tok, "utf8") > MAX_SUBCOMMAND_LEN) return false;
  if (/[^\x00-\x7f]/.test(tok)) return false;
  if (/[/\\=$"'@:.`]/.test(tok)) return false;
  if (/[0-9]/.test(tok)) return false;
  return (tok.match(/[-_]/g)?.length ?? 0) < 2;
}

// Separator-agnostic: a Windows path reported from any platform renders
// as its basename.
function baseName(p: string): string {
  const i = Math.max(p.lastIndexOf("/"), p.lastIndexOf("\\"));
  return i >= 0 ? p.slice(i + 1) : p;
}

// The host only — the path and query are where tokens live. JS's URL
// lowercases the host and drops a default port where Go's url.Parse
// keeps both; the shared vectors pin that difference.
function urlHost(raw: string): string {
  try {
    return new URL(raw).host;
  } catch {
    return "";
  }
}

// runEnd classifies a run by its final assistant message: "error" (with
// the error text), "aborted" (the user pressed Esc — not a failure), or
// "done". Wrapped for the same reason as lastAssistantText: a shape
// change in Pi must degrade to "done", not cost the event.
export function runEnd(messages: any): { kind: "done" | "aborted" | "error"; text: string } {
  try {
    for (let i = (messages?.length ?? 0) - 1; i >= 0; i--) {
      const msg = messages[i];
      if (msg?.role !== "assistant") continue;
      if (msg.stopReason === "aborted") return { kind: "aborted", text: "" };
      if (msg.stopReason !== "error") break;
      const text = typeof msg.errorMessage === "string" && msg.errorMessage ? msg.errorMessage : "error";
      return { kind: "error", text };
    }
  } catch {
    // ignore
  }
  return { kind: "done", text: "" };
}

// lastAssistantText digs the most recent assistant message's text out
// of the session so the tile can show what Pi just said. The shape is
// Pi's session format: getBranch() returns entries, and a message entry
// is {type:"message", message:{role, content}} where an assistant
// message's content is an array of typed blocks. Every step is
// optional-chained and the whole walk is wrapped: this reads another
// package's data shape, and a rename there must cost us the summary,
// not the turn_end event.
export function lastAssistantText(ctx: any): string {
  try {
    const entries = ctx?.sessionManager?.getBranch?.() ?? [];
    for (let i = entries.length - 1; i >= 0; i--) {
      const msg = entries[i]?.message;
      if (msg?.role !== "assistant") continue;
      const text = (msg.content ?? [])
        .filter((b: any) => b?.type === "text" && typeof b.text === "string")
        .map((b: any) => b.text)
        .join("")
        .trim();
      if (text) return text;
    }
  } catch {
    // ignore
  }
  return "";
}
