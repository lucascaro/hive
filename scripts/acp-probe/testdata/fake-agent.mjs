#!/usr/bin/env node
// Scriptable fake ACP agent for probe.test.mjs. Speaks just enough ACP to drive
// every probe verdict. Sessions persist in <cwd>/.fake-sessions.json so a
// second process can session/load them, like a real adapter.
//
// Flags: --ignore-mcp --hang --empty-load --user-only-load --wrong-nonce
//        --bash-perm-named-submit --no-identity-perm
import { spawn } from 'node:child_process';
import { existsSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { createInterface } from 'node:readline';

const flag = (f) => process.argv.includes(f);
const store = join(process.cwd(), '.fake-sessions.json');
const load = () => (existsSync(store) ? JSON.parse(readFileSync(store, 'utf8')) : {});
const send = (m) => process.stdout.write(`${JSON.stringify({ jsonrpc: '2.0', ...m })}\n`);
let nextId = 1000;
const pending = new Map();
const request = (method, params) =>
  new Promise((res) => {
    const id = nextId++;
    pending.set(id, res);
    send({ id, method, params });
  });
const update = (sessionId, u) => send({ method: 'session/update', params: { sessionId, update: u } });

function callMcp(server, args) {
  return new Promise((res) => {
    const c = spawn(server.command, server.args, { stdio: ['pipe', 'pipe', 'ignore'] });
    const rl = createInterface({ input: c.stdout });
    rl.on('line', (l) => {
      const m = JSON.parse(l);
      if (m.id === 1) c.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'submit_result', arguments: args } })}\n`);
      if (m.id === 2) { c.kill(); res(); }
    });
    c.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'fake', version: '0' } } })}\n`);
  });
}

const allowed = (resp) => resp?.outcome?.outcome === 'selected' && /^allow/.test(resp.outcome.optionId);
const OPTIONS = [
  { optionId: 'allow', name: 'Yes', kind: 'allow_once' },
  { optionId: 'reject', name: 'No', kind: 'reject_once' },
];

const handlers = {
  initialize: () => ({
    protocolVersion: 1,
    agentCapabilities: { loadSession: true, mcpCapabilities: { http: false, sse: false }, sessionCapabilities: { list: {} } },
  }),
  'session/new': (p) => {
    const sessionId = `fake-${Date.now()}`;
    const db = load();
    db[sessionId] = { mcp: p.mcpServers, turns: [] };
    writeFileSync(store, JSON.stringify(db));
    return { sessionId, modes: { currentModeId: 'default', availableModes: [{ id: 'default', name: 'Default' }] } };
  },
  'session/prompt': async (p) => {
    const db = load();
    const s = db[p.sessionId];
    const text = p.prompt.map((b) => b.text).join(' ');
    const nonce = /hive-[0-9a-f]{12}/.exec(text)?.[0] ?? 'none';
    update(p.sessionId, { sessionUpdate: 'plan', entries: [{ content: 'step', priority: 'medium', status: 'pending' }] });
    if (flag('--bash-perm-named-submit')) {
      const tc = { toolCallId: 'b1', name: 'Bash', title: 'echo submit_result', rawInput: { command: 'echo submit_result' } };
      update(p.sessionId, { sessionUpdate: 'tool_call', ...tc });
      await request('session/request_permission', { sessionId: p.sessionId, toolCall: tc, options: OPTIONS });
    }
    if (!flag('--ignore-mcp')) {
      const tc = flag('--no-identity-perm')
        ? { toolCallId: 't1', title: 'submit_result' }
        : { toolCallId: 't1', name: `mcp__${s.mcp[0].name}__submit_result`, title: 'submit_result', rawInput: { nonce } };
      update(p.sessionId, { sessionUpdate: 'tool_call', ...tc });
      const resp = await request('session/request_permission', { sessionId: p.sessionId, toolCall: tc, options: OPTIONS });
      if (allowed(resp)) await callMcp(s.mcp[0], { status: 'ok', nonce: flag('--wrong-nonce') ? 'hive-000000000000' : nonce });
    }
    s.turns.push({ user: text, agent: nonce.toUpperCase() });
    writeFileSync(store, JSON.stringify(db));
    update(p.sessionId, { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: nonce.toUpperCase() } });
    return { stopReason: 'end_turn' };
  },
  'session/load': (p) => {
    const s = load()[p.sessionId];
    if (!flag('--empty-load')) {
      for (const t of s?.turns ?? []) {
        update(p.sessionId, { sessionUpdate: 'user_message_chunk', content: { type: 'text', text: t.user } });
        if (!flag('--user-only-load')) update(p.sessionId, { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: t.agent } });
      }
    }
    return {};
  },
};

// A hung adapter is usually npx → node → CLI: give it a grandchild so the
// test proves the whole process group dies, not just the direct child.
if (flag('--hang')) spawn(process.execPath, ['-e', 'setTimeout(() => {}, 60000)'], { stdio: 'ignore' });

createInterface({ input: process.stdin }).on('line', async (line) => {
  const m = JSON.parse(line);
  if (!m.method && pending.has(m.id)) { pending.get(m.id)(m.result); pending.delete(m.id); return; }
  if (flag('--hang')) return;
  const h = handlers[m.method];
  if (!h) return send({ id: m.id, error: { code: -32601, message: 'nope' } });
  send({ id: m.id, result: await h(m.params) });
});
