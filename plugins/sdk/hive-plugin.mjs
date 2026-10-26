// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// hive-plugin.mjs — the Hive plugin SDK. One file, no dependencies,
// Node >= 18. Copy it into your plugin next to your entry point; it is
// vendored rather than published so a git-URL install needs no build
// or package install. See docs/plugins.md.
//
// A plugin is an ordinary Hive wire client. This file speaks the
// protocol for you:
//
//   import { connect } from './hive-plugin.mjs';
//   const hive = await connect();
//   hive.on('SESSION_EVENT', (ev) => console.log(ev.kind, ev.session.name));
//   hive.send('LIST_SESSIONS');
//
// Under Hive (HIVE_PLUGIN_ID set) the process exits when the daemon
// closes the connection — the daemon starts a fresh copy when it needs
// one, so a plugin that reconnected on its own would run twice.

import { EventEmitter } from 'node:events';
import net from 'node:net';

/** Wire protocol version this SDK speaks. */
export const PROTOCOL_VERSION = 1;

/**
 * Every frame type, by name. Mirrors internal/wire/frame.go; a Go test
 * (internal/plugin/sdk_drift_test.go) fails when the two disagree.
 * @type {Readonly<Record<string, number>>}
 */
export const Frame = Object.freeze({
  HELLO: 0x01,
  WELCOME: 0x02,
  DATA: 0x03,
  RESIZE: 0x04,
  EVENT: 0x05,
  ERROR: 0x06,
  LIST_SESSIONS: 0x07,
  SESSIONS: 0x08,
  CREATE_SESSION: 0x09,
  KILL_SESSION: 0x0a,
  UPDATE_SESSION: 0x0b,
  SESSION_EVENT: 0x0c,
  LIST_PROJECTS: 0x0d,
  PROJECTS: 0x0e,
  CREATE_PROJECT: 0x0f,
  KILL_PROJECT: 0x10,
  UPDATE_PROJECT: 0x11,
  PROJECT_EVENT: 0x12,
  RESTART_SESSION: 0x13,
  REQUEST_REPLAY: 0x14,
  SHUTDOWN: 0x15,
  LIST_WORKTREES: 0x16,
  WORKTREES: 0x17,
  REMOVE_WORKTREE: 0x18,
  CREATE_WORKTREE: 0x19,
  RENAME_WORKTREE: 0x1a,
  DELETE_BRANCH: 0x1b,
  RESTORE_SESSION: 0x1c,
  LIST_CLOSED: 0x1d,
  CLOSED: 0x1e,
  SESSION_RESTORED: 0x1f,
  CLIENT_COMMAND: 0x20,
  CLIENT_BROADCAST: 0x21,
  AGENT_EVENT: 0x22,
  LIST_IDEAS: 0x23,
  IDEAS: 0x24,
  ADD_IDEA: 0x25,
  UPDATE_IDEA: 0x26,
  REMOVE_IDEA: 0x27,
  IDEA_EVENT: 0x28,
  RESOLVE_PROMPT: 0x29,
  SET_WORKTREE_LABEL: 0x2a,
  GET_ACTIVITY: 0x2b,
  ACTIVITY: 0x2c,
  SEARCH_TRANSCRIPT: 0x2d,
  TRANSCRIPT_MATCHES: 0x2e,
  GET_TRANSCRIPT_LINES: 0x2f,
  TRANSCRIPT_LINES: 0x30,
  RESOLVE_WORKTREE_CHOICE: 0x31,
  PLAN_REVIEW_REQUEST: 0x32,
  PLAN_REVIEW_DECISION: 0x33,
  GET_PLAN_REVIEW: 0x34,
  PLAN_REVIEW: 0x35,
  RESOLVE_PLAN_REVIEW: 0x36,
  LIST_PLUGINS: 0x37,
  PLUGINS: 0x38,
  INSTALL_PLUGIN: 0x39,
  SET_PLUGIN_ENABLED: 0x3a,
  REMOVE_PLUGIN: 0x3b,
  PLUGIN_EVENT: 0x3c,
  SET_PLUGIN_CONFIG: 0x3d,
  SET_CLIENT_UI: 0x3e,
  GET_ACP_TRANSCRIPT: 0x3f,
  ACP_TRANSCRIPT: 0x40,
  PROMPT_ACP: 0x41,
  ANSWER_PERMISSION: 0x42,
  SUBMIT_RESULT: 0x43,
  SUBMIT_RESULT_OK: 0x44,
  SET_SESSION_KIND: 0x45,
  SEND_TO_SESSION: 0x46,
  SENT: 0x47,
  ARCHIVE_SESSION: 0x48,
  LIST_ARCHIVED: 0x49,
  ARCHIVED: 0x4a,
  UNARCHIVE_SESSION: 0x4b,
  DELETE_ARCHIVED: 0x4c,
  WAIT_SESSION: 0x4d,
  WAITED: 0x4e,
  ACK: 0x4f,
  CANCEL_ACP: 0x50,
  LIST_ACP_AGENTS: 0x51,
  ACP_AGENTS: 0x52,
});

const frameName = new Map(Object.entries(Frame).map(([k, v]) => [v, k]));

/**
 * The verbs request() accepts: the ones the daemon answers with a frame
 * carrying the req_id on success (an ACK, or the verb's own reply).
 * Every other verb answers success with a frame that has no id, so a
 * request() for it could only ever time out; it is refused up front.
 * Keep in step with internal/daemon (the ops.ack calls and the transcript
 * replies) and docs/plugins.md.
 */
export const REQUEST_VERBS = Object.freeze([
  'CREATE_SESSION',
  'KILL_SESSION',
  'RESTART_SESSION',
  'PROMPT_ACP',
  'CANCEL_ACP',
  'ANSWER_PERMISSION',
  'SEARCH_TRANSCRIPT',
  'GET_TRANSCRIPT_LINES',
  'LIST_ACP_AGENTS',
]);

/**
 * The frames that answer a request() by carrying its req_id. Anything
 * else with a req_id in its payload (a broadcast, say) is not a reply.
 */
const REPLY_FRAMES = new Set([
  Frame.ACK,
  Frame.ACP_AGENTS,
  Frame.TRANSCRIPT_MATCHES,
  Frame.TRANSCRIPT_LINES,
]);

/** Frames are capped at 1 MiB, like the daemon's. */
const MAX_PAYLOAD = 1 << 20;

// ---- wire payload shapes (JSDoc; checked against the Go structs) ----

/**
 * @typedef {object} Hello
 * @property {number} version
 * @property {string} client
 * @property {string} [build_id]
 * @property {'control'|'attach'|'create'} [mode]
 * @property {string} [session_id]
 * @property {object} [create]
 */

/**
 * @typedef {object} Welcome
 * @property {number} version
 * @property {string} [build_id]
 * @property {string} [release]
 * @property {number} [daemon_contract]
 * @property {string} [boot_id] random, fixed per hived process: a changed id means the daemon restarted
 * @property {string} [mode]
 * @property {string} [session_id]
 * @property {number} [cols]
 * @property {number} [rows]
 */

/**
 * @typedef {object} HiveError
 * @property {string} code
 * @property {string} message
 * @property {string} [session_id]
 * @property {string} [project_id]
 * @property {string} [nonce]
 * @property {number} [req_id] echoes the req_id of the request this answers
 */

/**
 * The reply to a request that has no reply frame of its own: what
 * {@link HiveConnection#request} resolves with.
 * @typedef {object} Ack
 * @property {number} req_id
 * @property {string} [session_id] CREATE_SESSION: the new session (it exists, but its worktree and adapter may still be starting)
 * @property {boolean} [was_busy] CANCEL_ACP: a turn was running and was cancelled (absent = false)
 * @property {number} [prompt_id] PROMPT_ACP: the transcript item id of the turn just started
 * @property {number} [epoch] PROMPT_ACP: the transcript epoch that prompt_id belongs to
 */

/**
 * @typedef {object} AcpAgentInfo
 * @property {string} id
 * @property {string} name
 * @property {object[]} modes {id, label} of each permission mode, least permissive first
 * @property {string} [default_mode]
 * @property {string} [ceiling] the most permissive mode the user allows
 * @property {boolean} [no_approval_gate]
 * @property {boolean} [experimental]
 * @property {boolean} available
 * @property {string} [reason] why not, when not available
 */

/**
 * @typedef {object} AcpAgents
 * @property {number} [req_id]
 * @property {AcpAgentInfo[]} agents
 */

/**
 * @typedef {object} SessionInfo
 * @property {string} id
 * @property {string} name
 * @property {string} [color]
 * @property {number} order
 * @property {string} [created]
 * @property {boolean} alive
 * @property {string} [agent]
 * @property {string} [project_id]
 * @property {string} [worktree_path]
 * @property {string} [worktree_branch]
 * @property {string} [last_error]
 * @property {string} [pending_prompt]
 * @property {object} [pending_worktree_choice]
 * @property {object} [pending_plan_review]
 * @property {string} [phase]
 * @property {string} [title]
 * @property {boolean} [needs_attention]
 * @property {boolean} [notify_when_idle]
 * @property {boolean} [orchestrator] the user let this session message its siblings (spec 389)
 * @property {''|'working'|'waiting_input'|'waiting_permission'|'exited'|'error'} [state]
 * @property {string} [state_source]
 * @property {string} [last_prompt]
 * @property {string} [last_summary]
 * @property {number} [plan_done]
 * @property {number} [plan_total]
 * @property {string} [current_tool]
 * @property {number} [subagents_running]
 * @property {string} [kind] "acp" for an ACP session; absent for a terminal session
 * @property {string} [spawned_by] who created the session: "plugin:<id>", "session:<id>", …; absent = the user
 */

/**
 * @typedef {object} SessionEvent
 * @property {'added'|'removed'|'updated'|'title'|'attention'|'state'} kind
 * @property {SessionInfo} session
 */

/**
 * @typedef {object} ProjectInfo
 * @property {string} id
 * @property {string} name
 * @property {string} [color]
 * @property {string} [cwd]
 * @property {number} order
 * @property {string} [created]
 * @property {Record<string, string>} [worktree_labels]
 */

/**
 * @typedef {object} ProjectEvent
 * @property {'added'|'removed'|'updated'} kind
 * @property {ProjectInfo} project
 */

/**
 * @typedef {object} PluginInfo
 * @property {string} id
 * @property {string} name
 * @property {string} version
 * @property {string} api_version
 * @property {string} [description]
 * @property {string} source
 * @property {string} [commit]
 * @property {string[]} command
 * @property {boolean} enabled
 * @property {'stopped'|'running'|'crashed'|'failed'|'refused'} status
 * @property {string} [status_detail]
 * @property {number} restarts
 * @property {{entry: string, style?: string}} [ui]
 * @property {object} [config]
 * @property {boolean} [builtin]
 */

/**
 * @typedef {object} PluginEvent
 * @property {'added'|'updated'|'removed'} kind
 * @property {PluginInfo} plugin
 * @property {string} [nonce]
 */

/**
 * @typedef {object} ClientCommand
 * @property {string} cmd
 * @property {string} [session_id]
 */

// ---- framing ----

function encode(type, payload) {
  const body =
    payload === undefined
      ? Buffer.alloc(0)
      : Buffer.isBuffer(payload)
        ? payload
        : Buffer.from(JSON.stringify(payload));
  if (body.length > MAX_PAYLOAD) throw new Error(`hive: frame exceeds ${MAX_PAYLOAD} bytes`);
  const head = Buffer.alloc(5);
  head[0] = type;
  head.writeUInt32BE(body.length, 1);
  return Buffer.concat([head, body]);
}

/** Splits a byte stream into [type, payload] frames. */
class FrameReader {
  constructor() {
    this.buf = Buffer.alloc(0);
  }
  /** @returns {Array<[number, Buffer]>} */
  push(chunk) {
    this.buf = this.buf.length ? Buffer.concat([this.buf, chunk]) : chunk;
    const out = [];
    while (this.buf.length >= 5) {
      const len = this.buf.readUInt32BE(1);
      if (len > MAX_PAYLOAD) throw new Error('hive: oversized frame from daemon');
      if (this.buf.length < 5 + len) break;
      out.push([this.buf[0], this.buf.subarray(5, 5 + len)]);
      this.buf = this.buf.subarray(5 + len);
    }
    return out;
  }
}

function socketPath(explicit) {
  const p = explicit ?? process.env.HIVE_SOCKET;
  if (!p) throw new Error('hive: HIVE_SOCKET is not set (run under Hive, or pass { socket })');
  return p;
}

function clientName() {
  const id = process.env.HIVE_PLUGIN_ID;
  return id ? `plugin/${id}` : 'plugin/standalone';
}

/**
 * Dials the daemon and completes the HELLO/WELCOME handshake.
 * @param {net.Socket} sock
 * @param {Partial<Hello>} hello
 * @returns {Promise<{welcome: Welcome, reader: FrameReader, rest: Array<[number, Buffer]>}>}
 */
function handshake(sock, hello) {
  return new Promise((resolve, reject) => {
    const reader = new FrameReader();
    const onData = (chunk) => {
      let frames;
      try {
        frames = reader.push(chunk);
      } catch (err) {
        cleanup();
        reject(err);
        return;
      }
      if (!frames.length) return;
      cleanup();
      const [type, body] = frames[0];
      const msg = JSON.parse(body.toString('utf8') || '{}');
      if (type === Frame.WELCOME) resolve({ welcome: msg, reader, rest: frames.slice(1) });
      else reject(Object.assign(new Error(`hive: ${msg.code}: ${msg.message}`), { hive: msg }));
    };
    const onErr = (err) => {
      cleanup();
      reject(err);
    };
    const cleanup = () => {
      // Pause before dropping the listener: a flowing stream with no
      // 'data' handler discards chunks, and the frames right after
      // WELCOME (the initial snapshot) must reach pump().
      sock.pause();
      sock.off('data', onData);
      sock.off('error', onErr);
      sock.off('close', onErr);
    };
    sock.on('data', onData);
    sock.on('error', onErr);
    sock.on('close', onErr);
    sock.write(
      encode(Frame.HELLO, { version: PROTOCOL_VERSION, client: clientName(), ...hello }),
    );
  });
}

function dial(path) {
  return new Promise((resolve, reject) => {
    const sock = net.createConnection(path);
    sock.once('connect', () => {
      sock.off('error', reject);
      resolve(sock);
    });
    sock.once('error', reject);
  });
}

/**
 * A control connection. Every frame the daemon sends is emitted as an
 * event named after its type ('SESSION_EVENT', 'PLUGIN_EVENT',
 * 'ERROR', …) with the decoded JSON payload.
 */
export class HiveConnection extends EventEmitter {
  /** @param {net.Socket} sock @param {Welcome} welcome @param {string} path */
  constructor(sock, welcome, path) {
    super();
    this.sock = sock;
    this.welcome = welcome;
    this.path = path;
    /** req_id -> {resolve, reject, timer}, for request(). */
    this._pending = new Map();
    this._reqSeq = 0;
  }

  /**
   * Sends a control frame carrying a fresh `req_id` and resolves with
   * the daemon's reply to it: an `ACK` (see the Ack typedef) or the
   * frame the request has of its own (`ACP_AGENTS`, `TRANSCRIPT_MATCHES`,
   * `TRANSCRIPT_LINES`), the decoded JSON payload. Rejects with an Error
   * whose `.hive` is the daemon's ERROR payload ({code, message, …}) if
   * it refused the request, on timeout, and when the connection closes.
   *
   * Only a reply frame type carrying the id settles a request.
   * Broadcasts (SESSION_EVENT, ACP_TRANSCRIPT, PLUGIN_EVENT, …) never do,
   * whatever their payload holds. `send()` is unchanged.
   *
   * Which frames reply is in docs/plugins.md; a frame that gets no
   * reply for the id would run into the timeout.
   *
   * @example
   * // CREATE_SESSION is acked as soon as the session exists; its
   * // worktree and adapter start afterwards (watch SESSION_EVENT).
   * const { session_id } = await hive.request('CREATE_SESSION', {
   *   kind: 'acp',
   *   agent: 'claude',
   *   workflow_run_id: 'run-1', // labels the session "plugin:<id>/workflow:run-1"
   *   result_schema: { type: 'object', properties: { ok: { type: 'boolean' } },
   *                    required: ['ok'], additionalProperties: false },
   * });
   * const { prompt_id, epoch } = await hive.request('PROMPT_ACP', { session_id, text: 'go', workflow_run_id: 'run-1' });
   * const { was_busy } = await hive.request('CANCEL_ACP', { session_id });
   *
   * @param {keyof typeof Frame} name
   * @param {object} [payload]
   * @param {{timeoutMs?: number}} [opts]
   * @returns {Promise<any>}
   */
  request(name, payload = {}, { timeoutMs = 30_000 } = {}) {
    const type = Frame[name];
    if (type === undefined) return Promise.reject(new Error(`hive: unknown frame ${name}`));
    if (!REQUEST_VERBS.includes(name)) {
      return Promise.reject(
        Object.assign(
          new Error(
            `hive: ${name} has no reply that carries a req_id; use send() and watch its events (request() supports ${REQUEST_VERBS.join(', ')})`,
          ),
          { hive: { code: 'unsupported_request', message: `${name} is not a request() verb` } },
        ),
      );
    }
    const reqId = ++this._reqSeq;
    return new Promise((resolve, reject) => {
      const timer = setTimeout(() => {
        this._pending.delete(reqId);
        reject(
          Object.assign(new Error(`hive: ${name} timed out after ${timeoutMs}ms`), {
            hive: { code: 'timeout', message: `no reply within ${timeoutMs}ms`, req_id: reqId },
          }),
        );
      }, timeoutMs);
      this._pending.set(reqId, { resolve, reject, timer });
      try {
        this.sock.write(encode(type, { ...payload, req_id: reqId }));
      } catch (err) {
        clearTimeout(timer);
        this._pending.delete(reqId);
        reject(err);
      }
    });
  }

  /** Settles the pending request a decoded frame answers, if any. */
  _settle(type, msg) {
    const id = msg?.req_id;
    const p = typeof id === 'number' ? this._pending.get(id) : undefined;
    if (!p) return;
    if (type === Frame.ERROR) {
      this._pending.delete(id);
      clearTimeout(p.timer);
      p.reject(Object.assign(new Error(`hive: ${msg.code}: ${msg.message}`), { hive: msg }));
    } else if (REPLY_FRAMES.has(type)) {
      this._pending.delete(id);
      clearTimeout(p.timer);
      p.resolve(msg);
    }
  }

  /** Rejects every pending request, when the connection is gone. */
  _rejectAll(err) {
    for (const [id, p] of this._pending) {
      clearTimeout(p.timer);
      p.reject(err);
      this._pending.delete(id);
    }
  }

  /**
   * Sends one control frame. Every control verb a GUI has is available:
   * see the table in docs/plugins.md.
   * @param {keyof typeof Frame} name
   * @param {object} [payload]
   */
  send(name, payload = {}) {
    const type = Frame[name];
    if (type === undefined) throw new Error(`hive: unknown frame ${name}`);
    this.sock.write(encode(type, payload));
  }

  /**
   * Opens an attach connection to one session — how a plugin types into
   * it. Output arrives as 'data' events (Buffers).
   * @param {string} sessionId
   */
  attach(sessionId) {
    return openAttach(this.path, sessionId);
  }

  close() {
    this.sock.end();
  }
}

/**
 * An attach connection to one session's terminal.
 */
export class Attachment extends EventEmitter {
  /** @param {net.Socket} sock */
  constructor(sock) {
    super();
    this.sock = sock;
  }
  /** Types into the session. @param {string|Buffer} data */
  write(data) {
    this.sock.write(encode(Frame.DATA, Buffer.from(data)));
  }
  close() {
    this.sock.end();
  }
}

function pump(sock, reader, initial, onFrame, onClose) {
  for (const f of initial) onFrame(...f);
  sock.on('data', (chunk) => {
    let frames;
    try {
      frames = reader.push(chunk);
    } catch {
      sock.destroy();
      return;
    }
    for (const f of frames) onFrame(...f);
  });
  sock.on('close', onClose);
  sock.on('error', () => {});
  sock.resume();
}

async function openAttach(path, sessionId) {
  const sock = await dial(path);
  const { reader, rest } = await handshake(sock, { mode: 'attach', session_id: sessionId });
  const a = new Attachment(sock);
  // Deferred like connect(): the replay can arrive in the same read as
  // WELCOME, and the caller needs a turn to add its 'data' listener.
  // The socket stays paused (see handshake) until pump resumes it.
  setImmediate(() =>
    pump(
      sock,
      reader,
      rest,
      (type, body) => {
        if (type === Frame.DATA) a.emit('data', body);
        else a.emit(frameName.get(type) ?? 'frame', JSON.parse(body.toString('utf8') || '{}'));
      },
      () => a.emit('close'),
    ),
  );
  return a;
}

/**
 * Connects to Hive in control mode.
 *
 * Under Hive the process exits when the connection closes (see the top
 * of this file); pass { exitOnClose: false } to handle 'close' yourself.
 *
 * @param {{socket?: string, exitOnClose?: boolean}} [opts]
 * @returns {Promise<HiveConnection>}
 */
export async function connect(opts = {}) {
  const path = socketPath(opts.socket);
  const sock = await dial(path);
  const { welcome, reader, rest } = await handshake(sock, { mode: 'control' });
  const conn = new HiveConnection(sock, welcome, path);
  const exitOnClose = opts.exitOnClose ?? Boolean(process.env.HIVE_PLUGIN_ID);
  // Defer so the caller can attach listeners before the snapshot frames
  // that follow WELCOME are delivered.
  setImmediate(() =>
    pump(
      sock,
      reader,
      rest,
      (type, body) => {
        const name = frameName.get(type) ?? 'frame';
        const msg = JSON.parse(body.toString('utf8') || '{}');
        conn._settle(type, msg);
        conn.emit(name, msg);
      },
      () => {
        conn._rejectAll(new Error('hive: connection closed'));
        conn.emit('close');
        if (exitOnClose) process.exit(0);
      },
    ),
  );
  return conn;
}
