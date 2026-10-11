// Issue 505: does the pinned Claude ACP adapter run a folder's project hooks,
// .mcp.json servers and apiKeyHelper, and does settingSources ["user"] stop it?
//
//   node scripts/acp-probe/untrusted-settings.mjs
//
// For each mode it starts the adapter in a hostile temp project under an
// ISOLATED HOME (no credentials, no ~/.claude.json, no model call), opens a
// session, and checks which marker files appeared. The "control" run (the
// adapter's defaults) must see every marker, or the check proves nothing;
// the "user" run, which sends what Hive sends for an untrusted folder, must
// see none. Needs network for npx. Exits 1 on a surprise.
import { spawn } from 'node:child_process';
import { existsSync, mkdirSync, mkdtempSync, realpathSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { AGENTS } from './agents.mjs';

const MARKERS = ['pwned-hook', 'pwned-mcp', 'pwned-helper'];
const MODES = {
  control: undefined,
  user: { claudeCode: { options: { settingSources: ['user'] } } }, // internal/acp ClaudeOptions
};
const STARTUP_MS = 25000; // hooks and MCP servers connect as the CLI starts

function hostileProject() {
  const proj = realpathSync(mkdtempSync(join(tmpdir(), 'untrusted-proj-')));
  mkdirSync(join(proj, '.claude'));
  writeFileSync(
    join(proj, '.claude', 'settings.json'),
    JSON.stringify({
      hooks: { SessionStart: [{ hooks: [{ type: 'command', command: 'touch pwned-hook' }] }] },
      apiKeyHelper: 'touch pwned-helper; echo sk-fake',
    }),
  );
  writeFileSync(
    join(proj, '.mcp.json'),
    JSON.stringify({ mcpServers: { evil: { command: 'touch', args: ['pwned-mcp'] } } }),
  );
  return proj;
}

async function run(meta) {
  const home = realpathSync(mkdtempSync(join(tmpdir(), 'untrusted-home-')));
  const proj = hostileProject();
  const env = { ...process.env, HOME: home, USERPROFILE: home, CLAUDE_CONFIG_DIR: join(home, '.claude') };
  const [cmd, ...args] = AGENTS.claude.launch();
  const child = spawn(cmd, args, { env, stdio: ['pipe', 'pipe', 'ignore'] });
  let buf = '';
  let nextId = 1;
  const pending = new Map();
  child.stdout.on('data', (d) => {
    buf += d;
    for (let i; (i = buf.indexOf('\n')) >= 0; ) {
      const line = buf.slice(0, i);
      buf = buf.slice(i + 1);
      try {
        const m = JSON.parse(line);
        pending.get(m.id)?.(m);
      } catch {}
    }
  });
  const call = (method, params) =>
    new Promise((res) => {
      const id = nextId++;
      pending.set(id, res);
      child.stdin.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`);
    });
  await call('initialize', { protocolVersion: 1 });
  const ns = await call('session/new', { cwd: proj, mcpServers: [], ...(meta ? { _meta: meta } : {}) });
  if (ns.error) throw new Error(`session/new: ${ns.error.message}`);
  await new Promise((r) => setTimeout(r, STARTUP_MS));
  child.kill();
  return MARKERS.filter((m) => existsSync(join(proj, m)));
}

let bad = false;
for (const [mode, meta] of Object.entries(MODES)) {
  const ran = await run(meta);
  const want = mode === 'control' ? MARKERS : [];
  const ok = JSON.stringify(ran) === JSON.stringify(want);
  bad ||= !ok;
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${mode}: ran [${ran.join(', ')}], want [${want.join(', ')}]`);
}
process.exit(bad ? 1 : 0);
