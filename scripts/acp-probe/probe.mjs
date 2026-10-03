#!/usr/bin/env node
// ACP capability probe (spec 492). Evidence for docs/design-docs/acp-workflows.md.
//
//   node scripts/acp-probe/probe.mjs <claude|codex|pi> [--out <dir>]
//
// Runs one scripted ACP session against a pinned adapter in a fresh temp cwd,
// then checks, out-of-band where possible:
//   mcp        — an MCP server injected via session/new received our nonce
//   permission — the agent asks session/request_permission
//   streaming  — plan and tool_call updates arrive
//   load       — a NEW adapter process replays the agent's own turns
//   takeover   — (b) Hive's own ResumeArgs argv, in a real PTY, shows the
//                nonce; (a) the CLI's headless resume answers with it
// Spend: two model prompts per agent (the session prompt + headless resume),
// no retries. Only whitelisted fields reach the results file; raw protocol
// logs stay in an untracked temp dir.
import { spawn, spawnSync } from 'node:child_process';
import { randomBytes } from 'node:crypto';
import { existsSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, writeFileSync, appendFileSync } from 'node:fs';
import { homedir, tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { createInterface } from 'node:readline';
import { fileURLToPath } from 'node:url';
import { AGENTS } from './agents.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
export const SUBMIT_MCP = join(HERE, 'submit-mcp.mjs');

export const DEFAULT_BUDGETS = { init: 90_000, prompt: 180_000, load: 60_000, headless: 120_000, pty: 45_000 };

export const REASONS = new Set([
  'ok', 'out_file_nonce', 'wrong_nonce', 'no_call', 'no_structured_identity', 'timeout',
  'adapter_error', 'not_advertised', 'agent_replay_has_nonce', 'no_agent_replay', 'resume_no_replay',
  'fresh_session', 'nonce_on_screen', 'nothing_rendered', 'nonce_in_stdout', 'nonce_missing',
  'request_seen', 'none_seen', 'skipped', 'cli_error', 'writer_locked',
]);

class TimeoutError extends Error {}

// The probe usually runs from inside an agent session (Claude Code, Hive).
// Those markers leak into children and change their behaviour (claude turns
// transcript saving off under CLAUDE_CODE_CHILD_SESSION; HIVE_SOCKET would
// route hook events to the user's real daemon). Hive's daemon spawns agents
// without them, so the probe does too. Names only — values are never read.
// Session markers only — user preferences such as CLAUDE_CODE_ENABLE_TODO_TOOLS
// pass through untouched.
const INHERITED_MARKERS =
  /^(CLAUDECODE|CLAUDE_CODE_(CHILD_SESSION|ENTRYPOINT|EXECPATH|SSE_PORT|MESSAGING_[A-Z_]+|SESSION_[A-Z_]+)|CLAUDE_PID|CLAUDE_EFFORT|HIVE_SESSION_ID|HIVE_SOCKET)$/;
export function childEnv(extra = {}) {
  const env = Object.fromEntries(Object.entries(process.env).filter(([k]) => !INHERITED_MARKERS.test(k)));
  return { ...env, ...extra };
}

export function withTimeout(promise, ms, label) {
  let t;
  return Promise.race([
    promise.finally(() => clearTimeout(t)),
    new Promise((_, rej) => {
      t = setTimeout(() => rej(new TimeoutError(`${label} timed out after ${ms}ms`)), ms);
    }),
  ]);
}

// ---------- process group handling ----------

export function spawnGroup(argv, opts) {
  // detached: own process group, so a timeout can kill npx + node + the CLI.
  return spawn(argv[0], argv.slice(1), { ...opts, detached: true });
}

export async function killGroup(child) {
  if (!child || child.exitCode !== null || child.signalCode !== null) {
    try { process.kill(-child.pid, 'SIGKILL'); } catch { /* already gone */ }
    return;
  }
  const gone = new Promise((r) => child.once('exit', r));
  try { process.kill(-child.pid, 'SIGTERM'); } catch { /* already gone */ }
  await Promise.race([gone, new Promise((r) => setTimeout(r, 2000))]);
  try { process.kill(-child.pid, 'SIGKILL'); } catch { /* already gone */ }
}

// ---------- JSON-RPC over newline-delimited stdio ----------

export class Rpc {
  constructor(child, { onRequest, onNotify, log }) {
    this.child = child;
    this.next = 1;
    this.pending = new Map();
    this.log = log ?? (() => {});
    this.closed = false;
    child.on('exit', () => {
      this.closed = true;
      for (const { rej } of this.pending.values()) rej(new Error('adapter exited'));
      this.pending.clear();
    });
    createInterface({ input: child.stdout }).on('line', async (line) => {
      let msg;
      try { msg = JSON.parse(line); } catch { this.log('<- (non-json)', line); return; }
      this.log('<-', msg);
      if (msg.method && msg.id !== undefined) {
        let reply;
        try { reply = { result: await onRequest(msg.method, msg.params) }; }
        catch (e) { reply = { error: { code: -32603, message: String(e.message ?? e) } }; }
        this.write({ id: msg.id, ...reply });
      } else if (msg.method) {
        onNotify(msg.method, msg.params);
      } else if (this.pending.has(msg.id)) {
        const { res, rej } = this.pending.get(msg.id);
        this.pending.delete(msg.id);
        msg.error ? rej(Object.assign(new Error(msg.error.message), { rpc: msg.error })) : res(msg.result);
      }
    });
  }
  write(msg) {
    if (this.closed) return;
    const full = { jsonrpc: '2.0', ...msg };
    this.log('->', full);
    this.child.stdin.write(`${JSON.stringify(full)}\n`);
  }
  request(method, params) {
    const id = this.next++;
    return new Promise((res, rej) => {
      this.pending.set(id, { res, rej });
      this.write({ id, method, params });
    });
  }
}

// ---------- pure decision logic (unit tested) ----------

// The structured identity of a tool call — never its title or other free text,
// which the agent controls (a Bash call can be *titled* "submit_result").
// Looks at the permission request's toolCall and the earlier tool_call update
// with the same toolCallId.
export function toolIdentity(toolCall, seen = new Map()) {
  const sources = [toolCall, seen.get(toolCall?.toolCallId)].filter(Boolean);
  for (const tc of sources) {
    // ACP's own tool-call name first (stable since schema 1.x), then vendor _meta.
    if (typeof tc.name === 'string') return { field: 'name', value: tc.name };
    const meta = tc._meta && typeof tc._meta === 'object' ? tc._meta : {};
    for (const [ns, v] of Object.entries(meta)) {
      if (!v || typeof v !== 'object') continue;
      for (const k of ['toolName', 'tool_name', 'name']) {
        if (typeof v[k] === 'string') return { field: `_meta.${ns}.${k}`, value: v[k] };
      }
    }
    if (typeof tc.toolName === 'string') return { field: 'toolName', value: tc.toolName };
    const ri = tc.rawInput;
    if (ri && typeof ri === 'object' && typeof ri.server === 'string' && typeof ri.tool === 'string') {
      return { field: 'rawInput.server+tool', value: `${ri.server}.${ri.tool}` };
    }
  }
  return null;
}

// Answer a session/request_permission. Allows ONLY the exact injected tool.
export function decidePermission(params, seen, submitIds) {
  const id = toolIdentity(params?.toolCall, seen);
  const allowed = id !== null && submitIds.has(id.value);
  const options = params?.options ?? [];
  const pick = (kinds) => kinds.map((k) => options.find((o) => o.kind === k)).find(Boolean);
  const opt = allowed ? pick(['allow_once', 'allow_always']) : pick(['reject_once', 'reject_always']);
  const response = opt
    ? { outcome: { outcome: 'selected', optionId: opt.optionId } }
    : { outcome: { outcome: 'cancelled' } };
  return { response, allowed, identity: id, reason: id === null ? 'no_structured_identity' : allowed ? 'ok' : 'rejected' };
}

export function readOutFile(path) {
  if (!existsSync(path)) return [];
  return readFileSync(path, 'utf8').split('\n').filter(Boolean).flatMap((l) => {
    try { return [JSON.parse(l)]; } catch { return []; }
  });
}

// MCP verdict from the out-of-band file the injected server wrote.
export function mcpVerdict(calls, nonce, permissions) {
  if (calls.some((c) => c?.nonce === nonce)) return { verdict: 'pass', reason: 'out_file_nonce' };
  if (calls.length > 0) return { verdict: 'fail', reason: 'wrong_nonce' };
  if (permissions.some((p) => p.reason === 'no_structured_identity')) {
    return { verdict: 'inconclusive', reason: 'no_structured_identity' };
  }
  return { verdict: 'fail', reason: 'no_call' };
}

const AGENT_AUTHORED = new Set(['agent_message_chunk', 'agent_thought_chunk', 'tool_call', 'tool_call_update']);

// Load verdict: the replay must include an AGENT-authored update holding the
// nonce. The nonce is also in our own prompt, so a replayed user_message_chunk
// proves nothing about the agent's turns persisting.
export function loadVerdict(replayed, nonce) {
  const hit = replayed.some((u) => {
    if (!AGENT_AUTHORED.has(u?.sessionUpdate)) return false;
    const s = JSON.stringify(u);
    return s.includes(nonce) || s.includes(nonce.toUpperCase());
  });
  return hit ? { verdict: 'pass', reason: 'agent_replay_has_nonce' } : { verdict: 'fail', reason: 'no_agent_replay' };
}

// What the PTY check looks for: the agent's own phase-1 reply, exactly as ACP
// delivered it — not a value we derive. (Observed: codex "uppercased" the
// nonce with a Cyrillic Е, U+0415, so a derived marker would miss a transcript
// that rendered fine.) Must not be a substring of our prompt, or the echoed
// prompt alone could satisfy it; falls back to the uppercased nonce.
export function replyMarker(reply, prompt, nonce) {
  const r = (reply ?? '').replace(/\s+/g, '');
  return r.length >= 8 && !prompt.replace(/\s+/g, '').includes(r) ? r : nonce.toUpperCase();
}

export function streamingVerdict(kinds) {
  const plan = kinds.has('plan');
  const tool = kinds.has('tool_call');
  return { verdict: plan && tool ? 'yes' : plan || tool ? 'partial' : 'no', reason: 'ok' };
}

const SAFE_ID = /^[\w.:-]{1,48}$/;
const safeIds = (xs) => [...new Set((xs ?? []).filter((x) => typeof x === 'string' && SAFE_ID.test(x)))].sort();
const VERDICTS = new Set(['pass', 'fail', 'inconclusive', 'unsupported', 'yes', 'no', 'partial', 'skipped']);
const check = (c) => ({
  verdict: VERDICTS.has(c?.verdict) ? c.verdict : 'inconclusive',
  reason: REASONS.has(c?.reason) ? c.reason : 'skipped',
  ...(c?.branch === 'resume' || c?.branch === 'session-id' ? { branch: c.branch } : {}),
  ...(c?.hiveBranch === 'resume' || c?.hiveBranch === 'session-id' ? { hive_branch: c.hiveBranch } : {}),
});
const bool = (x) => x === true || (x !== null && typeof x === 'object');

// The ONLY shape that reaches results/*.json. Built field by field from the
// internal result: no raw payloads, stderr, transcripts, paths, auth data.
export function whitelist(r) {
  const caps = r.capabilities ?? {};
  const sc = caps.sessionCapabilities ?? {};
  const version = (s) => (typeof s === 'string' && /\d+\.\d+\.\d+[\w.-]*/.exec(s)?.[0]) || null;
  return {
    agent: SAFE_ID.test(r.agent ?? '') ? r.agent : null,
    date: /^\d{4}-\d{2}-\d{2}$/.test(r.date ?? '') ? r.date : null,
    route: r.route === 'native' ? 'native' : 'adapter',
    adapter: typeof r.adapter === 'string' && /^[@\w./-]+@\d+\.\d+\.\d+$/.test(r.adapter) ? r.adapter : null,
    cli_version: version(r.cliVersion),
    capabilities: {
      loadSession: caps.loadSession === true,
      session: Object.fromEntries(['list', 'resume', 'close', 'delete', 'fork'].map((k) => [k, bool(sc[k])])),
      mcp: { http: caps.mcpCapabilities?.http === true, sse: caps.mcpCapabilities?.sse === true },
    },
    modes: safeIds(r.modes),
    config_options: safeIds(r.configOptions),
    update_kinds: safeIds([...(r.updateKinds ?? [])]),
    identity_field: typeof r.identityField === 'string' && SAFE_ID.test(r.identityField) ? r.identityField : null,
    session_id_mismatch: r.sessionIdMismatch === true,
    detached_daemons_after_pty: Number.isInteger(r.detachedDaemons) ? r.detachedDaemons : null,
    checks: {
      mcp: check(r.checks?.mcp),
      permission: check(r.checks?.permission),
      streaming: check(r.checks?.streaming),
      load: check(r.checks?.load),
      takeover_headless: check(r.checks?.takeover_headless),
      takeover_pty: check(r.checks?.takeover_pty),
    },
  };
}

// ---------- takeover checks ----------

// eslint-disable-next-line no-control-regex
const ANSI = /\x1b\[[0-9;?]*[ -/]*[@-~]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)|\x1b[@-_]/g;
export const stripAnsi = (s) => s.replace(ANSI, '');

// (b) Hive's own ResumeArgs argv in a real PTY (pty-run.py), stdin held open.
export const PTY_RUN = join(HERE, 'pty-run.py');
export async function ptyTakeover({ argv, branch, hiveBranch = branch, cwd, marker, trustPrompts, budget, rawDir }) {
  if (branch === 'session-id') return { verdict: 'fail', reason: 'fresh_session', branch, hiveBranch };
  const log = join(rawDir, 'pty.log');
  writeFileSync(log, '');
  const child = spawnGroup(['python3', PTY_RUN, log, '120', '40', '--', ...argv], {
    cwd, stdio: ['pipe', 'ignore', 'ignore'], env: childEnv({ TERM: 'xterm-256color' }),
  });
  const answered = new Set();
  let sentAt = 0;
  const deadline = Date.now() + budget;
  try {
    while (Date.now() < deadline && child.exitCode === null) {
      await new Promise((r) => setTimeout(r, 500));
      // TUIs position with cursor moves, not spaces: compare whitespace-free.
      const screen = stripAnsi(readFileSync(log, 'utf8')).replace(/\s+/g, '');
      if (screen.includes(marker)) return { verdict: 'pass', reason: 'nonce_on_screen', branch, hiveBranch };
      // Only output that arrived after our last keystroke: a dialog we already
      // dismissed must not keep matching (or keep blocking Enter) forever.
      const keys = dialogKeys(screen.slice(sentAt), trustPrompts, answered);
      if (keys) { child.stdin.write(keys); sentAt = screen.length; }
    }
    return { verdict: 'inconclusive', reason: 'nothing_rendered', branch, hiveBranch };
  } finally {
    await killGroup(child);
  }
}

// Which keystroke (if any) to send for the dialog now on screen. `tail` is
// the recent, whitespace-free screen. A `blocksEnter` dialog (codex's update
// nag, whose default runs curl | sh) pre-empts every other rule while it is
// visible, so an Enter meant for a trust prompt can never land on it.
export function dialogKeys(tail, prompts, answered = new Set()) {
  const live = prompts.map((p, i) => [p, i]).filter(([p]) => p.re.test(tail));
  const blocking = live.filter(([p]) => p.blocksEnter);
  for (const [p, i] of blocking.length ? blocking : live) {
    if (!answered.has(i)) { answered.add(i); return p.keys; }
  }
  return null;
}

// (a) The CLI's own headless resume, one prompt, same cwd.
// PIDs of processes whose command line matches re. Used to report (never
// kill) daemons a CLI detaches from its own process group.
export function pidsMatching(re) {
  const r = spawnSync('ps', ['-axo', 'pid=,command='], { encoding: 'utf8' });
  return new Set((r.stdout ?? '').split('\n').filter((l) => re.test(l)).map((l) => Number.parseInt(l, 10)));
}

export function headlessTakeover({ argv, cwd, nonce, budget }) {
  const r = spawnSync(argv[0], argv.slice(1), { cwd, encoding: 'utf8', timeout: budget, stdio: ['ignore', 'pipe', 'pipe'], env: childEnv() });
  if (r.error?.code === 'ETIMEDOUT' || r.signal) return { verdict: 'inconclusive', reason: 'timeout' };
  if (r.error) return { verdict: 'inconclusive', reason: 'cli_error' };
  // codex: a thread has one writer; a detached app-server left behind by an
  // earlier resume keeps it (see the design doc's findings).
  if (/already has an active writer/.test(r.stderr ?? '')) return { verdict: 'fail', reason: 'writer_locked' };
  return (r.stdout ?? '').includes(nonce)
    ? { verdict: 'pass', reason: 'nonce_in_stdout' }
    : { verdict: 'fail', reason: 'nonce_missing' };
}

// ---------- the probe ----------

async function openSession(def, cwd, rawLog, handlers) {
  const child = spawnGroup(def.launch(), { cwd, stdio: ['pipe', 'pipe', 'pipe'], env: childEnv() });
  child.stderr.on('data', (d) => appendFileSync(rawLog, `[stderr] ${d}`));
  const rpc = new Rpc(child, { ...handlers, log: (dir, m) => appendFileSync(rawLog, `${dir} ${typeof m === 'string' ? m : JSON.stringify(m)}\n`) });
  return { child, rpc };
}

const INIT = { protocolVersion: 1, clientCapabilities: { fs: { readTextFile: false, writeTextFile: false }, terminal: false } };

export async function runProbe(name, { def = AGENTS[name], budgets = DEFAULT_BUDGETS, home = homedir(), skipTakeover = false } = {}) {
  const cwd = realpathSync(mkdtempSync(join(tmpdir(), 'acp-probe-cwd-')));
  const rawDir = realpathSync(mkdtempSync(join(tmpdir(), 'acp-probe-raw-')));
  const rawLog = join(rawDir, 'rpc.log');
  const outFile = join(rawDir, 'submit.jsonl');
  const nonce = `hive-${randomBytes(6).toString('hex')}`;
  const r = {
    agent: name, date: new Date().toISOString().slice(0, 10), route: def.route,
    adapter: def.pkg ? `${def.pkg}@${def.version}` : null, checks: {}, updateKinds: new Set(),
    rawDir, cwd, pgids: [],
  };
  if (def.cli) {
    const v = spawnSync(def.cli[0], def.cli.slice(1), { encoding: 'utf8', timeout: 15_000, env: childEnv() });
    r.cliVersion = `${v.stdout ?? ''}`.split('\n')[0];
  }

  const seen = new Map();
  const permissions = [];
  const updates = [];
  let collect = updates;
  let promptText = '';
  const handlers = {
    onNotify: (method, params) => {
      if (method !== 'session/update' || !params?.update) return;
      const u = params.update;
      r.updateKinds.add(u.sessionUpdate);
      if (u.sessionUpdate === 'tool_call' && u.toolCallId) seen.set(u.toolCallId, u);
      collect.push(u);
    },
    onRequest: async (method, params) => {
      if (method === 'session/request_permission') {
        const d = decidePermission(params, seen, def.submitIds);
        permissions.push(d);
        if (d.identity) r.identityField = d.identity.field;
        return d.response;
      }
      throw new Error(`unsupported client method ${method}`);
    },
  };

  // Phase 1: initialize → session/new (MCP injected) → one prompt.
  let s = await openSession(def, cwd, rawLog, handlers);
  r.pgids.push(s.child.pid);
  let sessionId = null;
  try {
    const init = await withTimeout(s.rpc.request('initialize', INIT), budgets.init, 'initialize');
    r.capabilities = init?.agentCapabilities ?? {};
    const created = await withTimeout(s.rpc.request('session/new', {
      cwd,
      mcpServers: [{ name: 'hive', command: process.execPath, args: [SUBMIT_MCP, outFile], env: [] }],
    }), budgets.init, 'session/new');
    sessionId = created?.sessionId ?? null;
    r.modes = (created?.modes?.availableModes ?? []).map((m) => m.id);
    r.configOptions = (created?.configOptions ?? []).map((o) => o.id);
    promptText = `Record a short two-step plan using your built-in plan or todo tool if you have one. Then call the submit_result tool with {"status": "ok", "nonce": "${nonce}"}. Then reply with only the nonce ${nonce} written in UPPERCASE, and stop. Do not run any other tool.`;
    await withTimeout(s.rpc.request('session/prompt', {
      sessionId,
      prompt: [{ type: 'text', text: promptText }],
    }), budgets.prompt, 'session/prompt');
  } catch (e) {
    r.phase1Error = e instanceof TimeoutError ? 'timeout' : 'adapter_error';
    appendFileSync(rawLog, `[probe] phase1 ${e.message}\n`);
  } finally {
    await killGroup(s.child);
  }

  r.permissionLog = permissions.map((p) => ({ allowed: p.allowed, reason: p.reason, identity: p.identity?.value ?? null }));
  const failAll = { verdict: 'inconclusive', reason: r.phase1Error ?? 'skipped' };
  r.checks.mcp = sessionId ? mcpVerdict(readOutFile(outFile), nonce, permissions) : failAll;
  if (r.phase1Error && r.checks.mcp.verdict === 'fail') r.checks.mcp = failAll;
  r.checks.permission = permissions.length ? { verdict: 'yes', reason: 'request_seen' } : { verdict: 'no', reason: 'none_seen' };
  r.checks.streaming = streamingVerdict(r.updateKinds);

  // Phase 2: a NEW adapter process reloads the session.
  if (!sessionId) {
    r.checks.load = failAll;
  } else if (r.capabilities?.loadSession !== true) {
    r.checks.load = r.capabilities?.sessionCapabilities?.resume
      ? { verdict: 'inconclusive', reason: 'resume_no_replay' }
      : { verdict: 'unsupported', reason: 'not_advertised' };
  } else {
    const replayed = [];
    collect = replayed;
    s = await openSession(def, cwd, rawLog, handlers);
    r.pgids.push(s.child.pid);
    try {
      await withTimeout(s.rpc.request('initialize', INIT), budgets.init, 'initialize');
      await withTimeout(s.rpc.request('session/load', { sessionId, cwd, mcpServers: [] }), budgets.load, 'session/load');
      await new Promise((res) => setTimeout(res, 500)); // trailing notifications
      r.checks.load = loadVerdict(replayed, nonce);
    } catch (e) {
      r.checks.load = { verdict: 'inconclusive', reason: e instanceof TimeoutError ? 'timeout' : 'adapter_error' };
    } finally {
      await killGroup(s.child);
    }
  }

  // Phase 3: takeover — (b) Hive PTY first, while the session is untouched; then (a).
  if (!sessionId || skipTakeover) {
    r.checks.takeover_pty = r.checks.takeover_headless = skipTakeover ? { verdict: 'skipped', reason: 'skipped' } : failAll;
  } else {
    const cli = def.cliSessionId(sessionId, home);
    r.sessionIdMismatch = cli.mismatch;
    const { branch, hiveBranch, argv } = def.hiveResume(cli.id, cwd, home);
    const reply = updates
      .filter((u) => u.sessionUpdate === 'agent_message_chunk' && u.content?.type === 'text')
      .map((u) => u.content.text).join('');
    const before = def.daemonPattern ? pidsMatching(def.daemonPattern) : new Set();
    r.checks.takeover_pty = await ptyTakeover({ argv, branch, hiveBranch, cwd, marker: replyMarker(reply, promptText, nonce), trustPrompts: def.trustPrompts, budget: budgets.pty, rawDir });
    if (def.daemonPattern) {
      r.detachedDaemons = [...pidsMatching(def.daemonPattern)].filter((p) => !before.has(p)).length;
    }
    r.checks.takeover_headless = headlessTakeover({
      argv: def.headlessResume(cli.id, 'Reply with only the nonce you were given earlier in this conversation.'),
      cwd, nonce, budget: budgets.headless,
    });
  }
  return r;
}

// ---------- CLI ----------

if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const name = process.argv[2];
  const outIdx = process.argv.indexOf('--out');
  const outDir = outIdx > 0 ? process.argv[outIdx + 1] : join(HERE, 'results');
  if (!AGENTS[name]) {
    process.stderr.write(`usage: probe.mjs <${Object.keys(AGENTS).join('|')}> [--out <dir>]\n`);
    process.exit(2);
  }
  const r = await runProbe(name);
  const result = whitelist(r);
  mkdirSync(outDir, { recursive: true });
  const file = join(outDir, `${name}-${result.date}.json`);
  writeFileSync(file, `${JSON.stringify(result, null, 2)}\n`);
  process.stdout.write(`${JSON.stringify(result.checks, null, 2)}\nwrote ${file}\nraw logs (untracked): ${r.rawDir}\nprobe cwd: ${r.cwd}\n`);
}
