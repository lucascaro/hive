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
// A report is one connection: HELLO{mode:"event"} then one
// AGENT_EVENT, then close. The daemon replies to neither, so nothing
// here ever reads from the socket.
import net from "node:net";
import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";

const FRAME_HELLO = 0x01;
const FRAME_AGENT_EVENT = 0x22;
const PROTOCOL_VERSION = 1;

// Mirrors wire.MaxSummaryLen. The daemon truncates again on receipt;
// capping here is what keeps a pasted-file-sized prompt under
// wire.MaxPayload, where an oversized frame would be refused whole and
// lose the kind along with the text.
const MAX_SUMMARY_LEN = 512;

function frame(type: number, payload: unknown): Buffer {
  const body = Buffer.from(JSON.stringify(payload), "utf8");
  const head = Buffer.alloc(5);
  head.writeUInt8(type, 0);
  head.writeUInt32BE(body.length, 1);
  return Buffer.concat([head, body]);
}

// encodeFrames builds the two frames one report consists of. Exported
// so a Go test can decode what this encoder actually produces rather
// than trusting a hand-written fixture to stay in sync.
export function encodeFrames(sessionId: string, kind: string, text: string, at: string): Buffer {
  const hello = frame(FRAME_HELLO, {
    version: PROTOCOL_VERSION,
    client: "hive-pi-ext",
    mode: "event",
  });
  const event = frame(FRAME_AGENT_EVENT, {
    session_id: sessionId,
    kind,
    source: "extension",
    ...(text ? { text: truncate(text) } : {}),
    at,
  });
  return Buffer.concat([hello, event]);
}

// truncate cuts to MAX_SUMMARY_LEN *bytes* (the Go side's unit),
// backing off any partial UTF-8 sequence the cut created.
//
// The cut walks back over continuation bytes (10xxxxxx) rather than
// decoding and stripping a trailing replacement char: a U+FFFD the user
// actually typed is real text, and dropping it would make this differ
// from the Go side's strings.ToValidUTF8, which only rewrites bytes
// that are genuinely invalid.
export function truncate(s: string): string {
  const b = Buffer.from(s, "utf8");
  if (b.length <= MAX_SUMMARY_LEN) return s;
  let end = MAX_SUMMARY_LEN;
  while (end > 0 && (b[end] & 0xc0) === 0x80) end--;
  return b.subarray(0, end).toString("utf8");
}

export default function (pi: ExtensionAPI) {
  const sid = process.env.HIVE_SESSION_ID;
  const sock = process.env.HIVE_SOCKET;
  if (!sid || !sock) return; // not under Hive: inert

  // Fire-and-forget. Every failure mode here — no daemon, a wedged
  // daemon, a socket that vanished with a restarted hived — must look
  // exactly like no extension was loaded, never like a Pi error.
  const post = (kind: string, text = "") => {
    // Stamped here, not in the connect callback: `at` is when Pi
    // observed this (wire.AgentEvent's documented contract), and the
    // daemon orders events by it. Stamping at connect time would make
    // the stamp track connect order — the same order delivery already
    // has — which is exactly what the ordering guard exists to correct.
    const at = new Date().toISOString();
    try {
      const conn = net.createConnection(sock);
      conn.on("error", () => {});
      conn.setTimeout(2000, () => conn.destroy());
      conn.on("connect", () => {
        conn.end(encodeFrames(sid, kind, text, at));
      });
    } catch {
      // ignore
    }
  };

  pi.on("session_start", () => {
    post("ping");
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

  pi.on("agent_start", () => {
    turnInFlight = true;
    post("permission_resolved");
  });

  // agent_end can be followed by an auto-retry or a queued follow-up;
  // agent_settled is the one that means Pi has stopped on its own.
  pi.on("agent_settled", (_event, ctx) => {
    turnInFlight = false;
    post("turn_end", lastAssistantText(ctx));
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
  // since only PTY output can demote it.
  pi.on("ui_prompt_end", (_event, ctx) => {
    if (turnInFlight) post("permission_resolved");
    else post("turn_end", lastAssistantText(ctx));
  });

  // Only "quit" ends the pi process. "new", "resume", "fork" and
  // "reload" tear down the session runtime *inside a live pi* and
  // immediately stand another one up, so reporting session_end for them
  // would be a lie with no way back: StateExited is terminal in
  // agentstate.Machine.Apply, which drops every later event, and the
  // PTY is still very much alive.
  //
  // The replacement path reports turn_end rather than nothing, because
  // the command that triggered it (`/new`) arrives as an `input` event
  // first and has already moved the session to working. Posting nothing
  // would strand it there until the tier goes stale. turn_end with no
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
    else post("turn_end");
  });
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
