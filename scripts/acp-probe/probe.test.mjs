// node --test 'scripts/acp-probe/*.test.mjs'
// Everything here runs against testdata/fake-agent.mjs and temp dirs — no real
// agent CLI, no network, no model spend, no access to the user's real HOME.
import assert from 'node:assert/strict';
import { spawn } from 'node:child_process';
import { mkdirSync, mkdtempSync, readFileSync, realpathSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { createInterface } from 'node:readline';
import { after, test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { AGENTS, encodeClaudeProjectDirObserved, piResolveSessionId, submitIdsFor } from './agents.mjs';
import { checkDoc, computeVerdict } from './check-doc.mjs';
import { SUBMIT_MCP, childEnv, decidePermission, dialogKeys, headlessTakeover, ptyTakeover, replyMarker, runProbe, whitelist } from './probe.mjs';

const HERE = dirname(fileURLToPath(import.meta.url));
const FAKE = join(HERE, 'testdata', 'fake-agent.mjs');
const made = [];
const tmp = (p) => {
  const d = realpathSync(mkdtempSync(join(tmpdir(), p)));
  made.push(d);
  return d;
};
after(() => { for (const d of made) rmSync(d, { recursive: true, force: true }); });
const BUDGETS = { init: 5000, prompt: 10_000, load: 5000, headless: 5000, pty: 5000 };
const fakeDef = (...flags) => ({
  route: 'adapter', pkg: 'fake', version: '0.0.0',
  launch: () => [process.execPath, FAKE, ...flags],
  cliSessionId: (id) => ({ id, mismatch: false }),
  trustPrompts: [],
});
// home is a temp dir so no code path can touch the real ~/.claude or ~/.pi.
// runProbe leaves its cwd and raw-log dir for a human to inspect; tests clean them.
const probe = async (...flags) => {
  const r = await runProbe('fake', { def: fakeDef(...flags), budgets: BUDGETS, skipTakeover: true, home: tmp('acp-home-') });
  made.push(r.cwd, r.rawDir);
  return r;
};

// ---------- submit-mcp ----------

function mcp(outFile, requests) {
  return new Promise((resolve) => {
    const c = spawn(process.execPath, [SUBMIT_MCP, outFile], { stdio: ['pipe', 'pipe', 'ignore'] });
    const replies = [];
    createInterface({ input: c.stdout }).on('line', (l) => {
      replies.push(JSON.parse(l));
      if (replies.length === requests.length) { c.kill(); resolve(replies); }
    });
    for (const r of requests) c.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', ...r })}\n`);
  });
}

test('submit-mcp: tools/list exposes submit_result with a JSON schema', async () => {
  const [, list] = await mcp(join(tmp('acp-t-'), 'o'), [
    { id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18' } },
    { id: 2, method: 'tools/list' },
  ]);
  const tool = list.result.tools.find((t) => t.name === 'submit_result');
  assert.ok(tool);
  assert.deepEqual(tool.inputSchema.required, ['status', 'nonce']);
});

test('submit-mcp: tools/call writes exact arguments to the out file', async () => {
  const out = join(tmp('acp-t-'), 'o');
  await mcp(out, [{ id: 1, method: 'tools/call', params: { name: 'submit_result', arguments: { status: 'ok', nonce: 'n1' } } }]);
  assert.deepEqual(JSON.parse(readFileSync(out, 'utf8')), { status: 'ok', nonce: 'n1' });
});

// ---------- probe against the fake agent ----------

test('probe: all pass against a well-behaved fake agent', async () => {
  const r = await probe();
  assert.equal(r.checks.mcp.verdict, 'pass');
  assert.equal(r.checks.load.verdict, 'pass');
  assert.equal(r.checks.permission.verdict, 'yes');
  assert.equal(r.checks.streaming.verdict, 'yes');
  assert.equal(r.identityField, 'name');
});

test('probe: mcp=fail when the agent ignores mcpServers', async () => {
  const r = await probe('--ignore-mcp');
  assert.deepEqual(r.checks.mcp, { verdict: 'fail', reason: 'no_call' });
});

test('probe: mcp=fail when the out file holds a different nonce', async () => {
  const r = await probe('--wrong-nonce');
  assert.deepEqual(r.checks.mcp, { verdict: 'fail', reason: 'wrong_nonce' });
});

test('probe: load=fail when loadSession is advertised but replays nothing', async () => {
  const r = await probe('--empty-load');
  assert.deepEqual(r.checks.load, { verdict: 'fail', reason: 'no_agent_replay' });
});

test('probe: load=fail when only the user prompt is replayed', async () => {
  const r = await probe('--user-only-load');
  assert.deepEqual(r.checks.load, { verdict: 'fail', reason: 'no_agent_replay' });
});

test('probe: rejects permission for a Bash call whose title/input contains "submit_result"', async () => {
  const r = await probe('--bash-perm-named-submit');
  const bash = r.permissionLog.find((p) => p.identity === 'Bash');
  assert.equal(bash.allowed, false);
  assert.equal(r.checks.mcp.verdict, 'pass'); // the real tool still went through
});

test('probe: permission request with no structured identity → mcp=inconclusive', async () => {
  const r = await probe('--no-identity-perm');
  assert.deepEqual(r.checks.mcp, { verdict: 'inconclusive', reason: 'no_structured_identity' });
});

test('probe: hung agent times out, its process group is gone, and the probe returns', async () => {
  const r = await runProbe('fake', { def: fakeDef('--hang'), budgets: { ...BUDGETS, init: 1500 }, skipTakeover: true, home: tmp('acp-home-') });
  assert.equal(r.phase1Error, 'timeout');
  assert.equal(r.checks.mcp.verdict, 'inconclusive');
  for (const pgid of r.pgids) assert.throws(() => process.kill(-pgid, 0), { code: 'ESRCH' });
});

// ---------- permission decisions ----------

const OPTS = [{ optionId: 'a', kind: 'allow_once' }, { optionId: 'r', kind: 'reject_once' }];

test('decidePermission: allows only the exact injected tool identity', () => {
  const ids = submitIdsFor('hive');
  assert.equal(decidePermission({ toolCall: { name: 'mcp__hive__submit_result' }, options: OPTS }, new Map(), ids).allowed, true);
  assert.equal(decidePermission({ toolCall: { _meta: { claudeCode: { toolName: 'mcp__hive__submit_result' } } }, options: OPTS }, new Map(), ids).allowed, true);
  for (const tc of [
    { name: 'submit_result' }, // bare name: could be any server's tool
    { name: 'mcp__evil__submit_result' },
    { name: 'Bash', title: 'mcp__hive__submit_result', rawInput: { command: 'mcp__hive__submit_result' } },
  ]) {
    const d = decidePermission({ toolCall: tc, options: OPTS }, new Map(), ids);
    assert.equal(d.allowed, false, JSON.stringify(tc));
    assert.deepEqual(d.response, { outcome: { outcome: 'selected', optionId: 'r' } });
  }
});

test('decidePermission: identity comes from the earlier tool_call update when the request omits it', () => {
  const seen = new Map([['t9', { toolCallId: 't9', name: 'mcp__hive__submit_result' }]]);
  assert.equal(decidePermission({ toolCall: { toolCallId: 't9', title: 'x' }, options: OPTS }, seen, submitIdsFor('hive')).allowed, true);
});

test('decidePermission: binds to the per-run server, so a user server named "hive" is rejected', () => {
  const d = decidePermission({ toolCall: { name: 'mcp__hive__submit_result' }, options: OPTS }, new Map(), submitIdsFor('hiveprobeabc123'));
  assert.equal(d.allowed, false);
});

test('probe: a missing adapter binary is reported, not a crash', async () => {
  const def = { ...fakeDef(), launch: () => ['acp-probe-no-such-binary-xyz'] };
  const r = await runProbe('fake', { def, budgets: BUDGETS, skipTakeover: true, home: tmp('acp-home-') });
  assert.equal(r.phase1Error, 'adapter_error');
  assert.equal(r.checks.mcp.verdict, 'inconclusive');
});

// ---------- takeover ----------

test('takeover: claude falls back to --session-id when no transcript exists, and that is a fail', async () => {
  const home = tmp('acp-home-');
  const h = AGENTS.claude.hiveResume('abc', '/tmp/x_y', home);
  assert.deepEqual([h.branch, h.hiveBranch], ['session-id', 'session-id']);
  const r = await ptyTakeover({ ...h, cwd: home, marker: 'X', trustPrompts: [], budget: 1000, rawDir: home });
  assert.deepEqual([r.verdict, r.reason], ['fail', 'fresh_session']);
});

test("takeover: claude transcript under claude's real encoding is found even where Hive's encoder misses it", () => {
  const home = tmp('acp-home-');
  const cwd = '/tmp/a_b.c';
  const dir = join(home, '.claude', 'projects', encodeClaudeProjectDirObserved(cwd));
  mkdirSync(dir, { recursive: true });
  writeFileSync(join(dir, 'abc.jsonl'), '{}\n');
  const h = AGENTS.claude.hiveResume('abc', cwd, home);
  assert.equal(h.branch, 'resume');
  assert.equal(h.hiveBranch, 'session-id'); // Hive's encoder keeps "_" → misses it
  assert.deepEqual(h.argv, ['claude', '--resume', 'abc']);
});

test('takeover: pi-acp id differs from the pi session id and is resolved through the mapping', () => {
  const home = tmp('acp-home-');
  mkdirSync(join(home, '.pi', 'pi-acp'), { recursive: true });
  writeFileSync(join(home, '.pi', 'pi-acp', 'session-map.json'), JSON.stringify({
    version: 1, sessions: { acp1: { sessionId: 'acp1', sessionFile: '/x/2026-10-02T00-00-00_pi-real.jsonl' } },
  }));
  assert.deepEqual(piResolveSessionId('acp1', home), { id: 'pi-real', mismatch: true, via: 'session-map' });
  assert.deepEqual(piResolveSessionId('other', home), { id: 'other', mismatch: false, via: 'none' });
});

const nodeArgv = (src) => [process.execPath, '-e', src];

test('pty: marker rendered in a real PTY → pass', async () => {
  const d = tmp('acp-pty-');
  const r = await ptyTakeover({ argv: nodeArgv('console.log("HIVE-OK"); setTimeout(()=>{},3000)'), branch: 'resume', cwd: d, marker: 'HIVE-OK', trustPrompts: [], budget: 5000, rawDir: d });
  assert.equal(r.verdict, 'pass');
});

test('pty: a trust dialog is answered through stdin, then the marker renders → pass', async () => {
  const d = tmp('acp-pty-');
  const src = 'process.stdout.write("Do you trust the files in this folder? "); process.stdin.once("data",()=>{console.log("HIVE-OK"); setTimeout(()=>process.exit(0),2000)})';
  const r = await ptyTakeover({ argv: nodeArgv(src), branch: 'resume', cwd: d, marker: 'HIVE-OK', trustPrompts: AGENTS.claude.trustPrompts, budget: 6000, rawDir: d });
  assert.equal(r.verdict, 'pass');
});

test('trust prompts: the codex update nag is answered with Esc, never Enter', () => {
  const screen = '1. Update now (runs `sh -c curl | sh`) 2. Skip 3. Skip until next version';
  const hits = AGENTS.codex.trustPrompts.filter((p) => p.re.test(screen.replace(/\s+/g, '')));
  assert.equal(hits.length, 1);
  assert.equal(hits[0].keys, '\x1b');
});

test('dialogKeys: never sends Enter while the update nag is on screen, even if a trust prompt is too', () => {
  const tail = 'Doyoutrustthefilesinthisfolder?1.Updatenow2.Skip';
  const answered = new Set();
  assert.equal(dialogKeys(tail, AGENTS.codex.trustPrompts, answered), '\x1b');
  assert.equal(dialogKeys(tail, AGENTS.codex.trustPrompts, answered), null); // nag answered once; still no Enter
  assert.equal(dialogKeys('Doyoutrustthefilesinthisfolder?', AGENTS.codex.trustPrompts, new Set()), '\r');
});

test('replyMarker: uses the agent reply verbatim, never text from our own prompt', () => {
  const prompt = 'reply with hive-abcdef123456 in UPPERCASE';
  assert.equal(replyMarker('HIVE-ABCDЕF123456', prompt, 'hive-abcdef123456'), 'HIVE-ABCDЕF123456'); // Cyrillic Е kept
  assert.equal(replyMarker('hive-abcdef123456', prompt, 'hive-abcdef123456'), 'HIVE-ABCDEF123456'); // echo of the prompt rejected
  assert.equal(replyMarker('', prompt, 'hive-abcdef123456'), 'HIVE-ABCDEF123456');
});

test('pty: matching ignores the whitespace a TUI replaces with cursor moves', async () => {
  const d = tmp('acp-pty-');
  const r = await ptyTakeover({ argv: nodeArgv('process.stdout.write("HIVE-\\x1b[2C-OK\\n"); setTimeout(()=>{},3000)'), branch: 'resume', cwd: d, marker: 'HIVE--OK', trustPrompts: [], budget: 4000, rawDir: d });
  assert.equal(r.verdict, 'pass');
});

test('pty: update nag dismissed with Esc, then a later trust prompt still gets Enter → pass', async () => {
  const d = tmp('acp-pty-');
  // Raw mode so single keys arrive unbuffered, as in a real TUI.
  const src = `process.stdin.setRawMode(true);
    process.stdout.write("1. Update now 2. Skip ");
    process.stdin.once("data", (k) => {
      if (k[0] !== 0x1b) { console.log("ENTER-ON-NAG"); process.exit(1); }
      process.stdout.write("Trust this folder? ");
      process.stdin.once("data", (k2) => { if (k2[0] === 13) console.log("HIVE-OK"); setTimeout(() => process.exit(0), 2000); });
    });`;
  const r = await ptyTakeover({ argv: nodeArgv(src), branch: 'resume', cwd: d, marker: 'HIVE-OK', trustPrompts: AGENTS.codex.trustPrompts, budget: 8000, rawDir: d });
  assert.equal(r.verdict, 'pass');
});

test('pty: the PTY child is killed too, although pty.fork gave it its own session', async () => {
  const d = tmp('acp-pty-');
  const pidFile = join(d, 'pid');
  // Ignores SIGHUP (as some TUIs do), so only the forwarded SIGTERM can stop it.
  const src = `process.on("SIGHUP", () => {}); require("fs").writeFileSync(${JSON.stringify(pidFile)}, String(process.pid)); setTimeout(() => {}, 30000)`;
  const r = await ptyTakeover({ argv: nodeArgv(src), branch: 'resume', cwd: d, marker: 'NEVER', trustPrompts: [], budget: 1500, rawDir: d });
  assert.equal(r.verdict, 'inconclusive');
  const pid = Number(readFileSync(pidFile, 'utf8'));
  await new Promise((res) => setTimeout(res, 500));
  assert.throws(() => process.kill(pid, 0), { code: 'ESRCH' });
});

test('pty: nothing recognisable within budget → inconclusive, never pass', async () => {
  const d = tmp('acp-pty-');
  const r = await ptyTakeover({ argv: nodeArgv('setTimeout(()=>{},10000)'), branch: 'resume', cwd: d, marker: 'HIVE-OK', trustPrompts: [], budget: 1500, rawDir: d });
  assert.deepEqual([r.verdict, r.reason], ['inconclusive', 'nothing_rendered']);
});

// ---------- headless takeover ----------

const headless = (argv, budget = 5000) => headlessTakeover({ argv, cwd: tmp('acp-headless-'), nonce: 'hive-n0nce', budget });

test('headless: the nonce on stdout → pass', async () => {
  assert.deepEqual(await headless(nodeArgv('console.log("it was hive-n0nce")')), { verdict: 'pass', reason: 'nonce_in_stdout' });
});

test('headless: a reply without the nonce → fail, never pass', async () => {
  assert.deepEqual(await headless(nodeArgv('console.log("I do not remember")')), { verdict: 'fail', reason: 'nonce_missing' });
});

test('headless: a single-writer lock on stderr → writer_locked', async () => {
  const r = await headless(nodeArgv('console.error("thread already has an active writer"); process.exit(1)'));
  assert.deepEqual(r, { verdict: 'fail', reason: 'writer_locked' });
});

test('headless: a detached daemon holding stdout cannot hang the check (exit + drain grace)', async () => {
  // Mimics codex: the CLI leaves a detached process that inherited its stdout,
  // so the pipe never closes. Waiting for 'close' alone would sit out the budget.
  const src = `require("child_process").spawn(process.execPath, ["-e", "setTimeout(() => {}, 8000)"], { stdio: "inherit", detached: true }).unref();
    console.log("hive-n0nce");`;
  const t = Date.now();
  const r = await headless([process.execPath, '-e', src], 6000);
  assert.equal(r.verdict, 'pass');
  assert.ok(Date.now() - t < 4000, `took ${Date.now() - t}ms`);
});

test('headless: a missing CLI binary → inconclusive cli_error, not a crash', async () => {
  assert.deepEqual(await headless(['/nonexistent/acp-probe-cli']), { verdict: 'inconclusive', reason: 'cli_error' });
});

test('headless: a hung CLI times out and its process group is gone', async () => {
  const pidFile = join(tmp('acp-headless-pid-'), 'pid');
  const r = await headless(nodeArgv(`require("fs").writeFileSync(${JSON.stringify(pidFile)}, String(process.pid)); setTimeout(()=>{},10000)`), 1500);
  assert.deepEqual(r, { verdict: 'inconclusive', reason: 'timeout' });
  const pid = Number(readFileSync(pidFile, 'utf8'));
  assert.throws(() => process.kill(-pid, 0), { code: 'ESRCH' });
});

test('childEnv: strips parent-session markers, keeps user preferences', () => {
  const saved = { ...process.env };
  Object.assign(process.env, { CLAUDECODE: '1', CLAUDE_CODE_CHILD_SESSION: '1', HIVE_SOCKET: '/x', CLAUDE_CODE_ENABLE_TODO_TOOLS: '1' });
  try {
    const env = childEnv();
    assert.equal(env.CLAUDECODE, undefined);
    assert.equal(env.CLAUDE_CODE_CHILD_SESSION, undefined);
    assert.equal(env.HIVE_SOCKET, undefined);
    assert.equal(env.CLAUDE_CODE_ENABLE_TODO_TOOLS, '1');
  } finally {
    for (const k of Object.keys(process.env)) if (!(k in saved)) delete process.env[k];
    Object.assign(process.env, saved);
  }
});

// ---------- results whitelist ----------

test('results: writer emits only whitelisted keys', () => {
  const out = whitelist({
    agent: 'claude', date: '2026-10-02', route: 'adapter', adapter: '@x/y@1.2.3', cliVersion: '2.1.288 (Claude Code)',
    capabilities: { loadSession: true, authMethods: [{ id: 'oauth', secret: 's3cr3t' }], sessionCapabilities: { list: {} } },
    modes: ['default', '/Users/someone/path', 'x'.repeat(80)], updateKinds: new Set(['plan']),
    checks: { mcp: { verdict: 'pass', reason: 'out_file_nonce', stderr: 'token=abc' } },
    rawDir: '/Users/someone/raw', cwd: '/Users/someone/cwd', permissionLog: [{ identity: 'x' }], stderr: 'secret',
  });
  const s = JSON.stringify(out);
  for (const leak of ['s3cr3t', 'oauth', '/Users/', 'token=', 'secret']) assert.ok(!s.includes(leak), leak);
  assert.deepEqual(Object.keys(out).sort(), [
    'adapter', 'agent', 'capabilities', 'checks', 'cli_version', 'config_options', 'date',
    'detached_daemons_after_pty', 'identity_field', 'modes', 'route', 'session_id_mismatch', 'update_kinds',
  ]);
  assert.deepEqual(out.modes, ['default']);
  assert.deepEqual(out.checks.mcp, { verdict: 'pass', reason: 'out_file_nonce' });
  assert.equal(out.cli_version, '2.1.288');
});

// ---------- check-doc ----------

const res = (mcpV, ptyV) => ({ adapter: '@a/b@1.0.0', checks: {
  mcp: { verdict: mcpV }, permission: { verdict: 'yes' }, streaming: { verdict: 'partial' },
  load: { verdict: 'pass' }, takeover_headless: { verdict: 'pass' }, takeover_pty: { verdict: ptyV },
} });

test('check-doc: computes go / no-go / undecided with the SC6 rule', () => {
  assert.equal(computeVerdict({ claude: res('pass', 'pass'), codex: res('fail', 'fail') }), 'go');
  assert.equal(computeVerdict({ claude: res('fail', 'pass'), codex: res('fail', 'pass') }), 'no-go');
  assert.equal(computeVerdict({ claude: res('pass', 'fail'), codex: res('pass', 'fail') }), 'no-go');
  assert.equal(computeVerdict({ claude: res('pass', 'inconclusive'), codex: res('inconclusive', 'inconclusive') }), 'undecided');
  assert.equal(computeVerdict({ claude: res('pass', 'fail'), codex: res('fail', 'pass') }), 'undecided');
});

function doc({ verdict = 'go', claudeMcp = 'pass [run]', piTag = 'run' } = {}) {
  const run = (v) => `${v} [run]`;
  const probed = (name, mcpCell = run('pass')) =>
    `| ${name} | adapter \`@a/b@1.0.0\` [${piTag === 'doc' && name === 'Pi' ? 'doc' : 'run'}] | ${mcpCell} | ${run('yes')} | ${run('partial')} | ${run('pass')} | ${run('pass')} | ${run('pass')} |`;
  const docRow = (name) => `| ${name} | native [doc] | yes [doc] | yes [doc] | yes [doc] | pass [doc] | unknown [doc] | unknown [doc] |`;
  return [
    '<!-- capability-table:start -->',
    '| Agent | Route | MCP | Perm | Stream | Load | Headless | PTY |',
    '|---|---|---|---|---|---|---|---|',
    probed('Claude', claudeMcp), probed('Codex'), docRow('Gemini'), docRow('Copilot'), probed('Pi'),
    '<!-- capability-table:end -->',
    `**Verdict: ${verdict}**`,
  ].join('\n');
}
const allPass = { claude: res('pass', 'pass'), codex: res('pass', 'pass'), pi: res('pass', 'pass') };

test('check-doc: a consistent doc passes', () => {
  assert.deepEqual(checkDoc(doc(), allPass), []);
});

test('check-doc: fails on a run cell that disagrees with results', () => {
  const errs = checkDoc(doc({ claudeMcp: 'fail [run]' }), allPass);
  assert.ok(errs.some((e) => e.includes('claude/mcp')), errs.join('\n'));
});

test('check-doc: fails when the doc verdict differs from the computed one', () => {
  assert.ok(checkDoc(doc({ verdict: 'no-go' }), allPass).some((e) => e.startsWith('go/no-go')));
});

test('check-doc: fails when a results file lacks a verdict for a [run] cell', () => {
  const partial = { ...allPass, codex: { ...res('pass', 'pass'), checks: { ...res('pass', 'pass').checks, load: undefined } } };
  assert.ok(checkDoc(doc(), partial).some((e) => e.includes('codex/load: results file has no verdict')));
});

test('check-doc: fails when a probed row is tagged doc or has no results', () => {
  assert.ok(checkDoc(doc({ piTag: 'doc' }), allPass).some((e) => e.includes('pi/route')));
  const { pi, ...noPi } = allPass;
  assert.ok(checkDoc(doc(), noPi).some((e) => e.includes('pi: probed agent has no results file')));
});
