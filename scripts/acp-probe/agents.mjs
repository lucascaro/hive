// Pinned agent table for the ACP probe (spec 492).
//
// Each entry says how to launch the agent over ACP and how Hive would reopen
// the same session in a PTY. The PTY argv MUST mirror Hive's own ResumeArgs
// in internal/agent — that is the path a Hive takeover actually takes
// (Registry.Revive, internal/registry/registry.go:1315 → ResumeArgs). When
// the Go side changes, grep for the citations below and update the mirror.
import { existsSync, readFileSync, readdirSync } from 'node:fs';
import { homedir } from 'node:os';
import { join, basename } from 'node:path';

// Mirrors encodeClaudeProjectDir (internal/agent/claude.go:40-61), which
// ports claude's own encoder (2.1.288): every UTF-16 unit outside
// [A-Za-z0-9] becomes "-", and a name over 200 is cut and suffixed with a
// base-36 hash of the raw cwd. Unlike the Go side this does not
// filepath.Clean the cwd first; the probe only passes clean temp paths.
export function encodeClaudeProjectDir(cwd) {
  const k = cwd.replace(/[^A-Za-z0-9]/g, '-');
  if (k.length <= 200) return k;
  let h = 0;
  for (let i = 0; i < cwd.length; i++) h = ((h << 5) - h + cwd.charCodeAt(i)) | 0;
  return `${k.slice(0, 200)}-${Math.abs(h).toString(36)}`;
}

// Mirrors claudeSessionExists (internal/agent/claude.go:72-83).
export function claudeSessionExists(id, cwd, home = homedir()) {
  return !!id && !!cwd && existsSync(join(home, '.claude', 'projects', encodeClaudeProjectDir(cwd), `${id}.jsonl`));
}

// Mirrors encodePiSessionsDir (internal/agent/pi.go:98). Unlike claude,
// pi keeps "." — reusing the claude encoder silently finds nothing.
export function encodePiSessionsDir(cwd) {
  const s = cwd.replace(/^\//, '').replaceAll('/', '-').replaceAll('\\', '-').replaceAll(':', '-');
  return `--${s}--`;
}

// pi-acp keeps ~/.pi/pi-acp/session-map.json: ACP sessionId → pi sessionFile.
// pi names files "<timestamp>_<pi-session-id>.jsonl" (internal/agent/pi.go:110).
// Returns the id `pi --session-id` must be given, and whether it differs from
// the ACP id (a mismatch Hive would have to resolve the same way).
export function piResolveSessionId(acpId, home = homedir()) {
  const mapPath = join(home, '.pi', 'pi-acp', 'session-map.json');
  let file = null;
  try {
    file = JSON.parse(readFileSync(mapPath, 'utf8'))?.sessions?.[acpId]?.sessionFile ?? null;
  } catch {
    /* no map: fall through */
  }
  if (!file) return { id: acpId, mismatch: false, via: 'none' };
  const m = /_([^_]+)\.jsonl$/.exec(basename(file));
  const id = m ? m[1] : acpId;
  return { id, mismatch: id !== acpId, via: 'session-map' };
}

export function piSessionExists(id, cwd, home = homedir()) {
  const dir = join(home, '.pi', 'agent', 'sessions', encodePiSessionsDir(cwd));
  try {
    return readdirSync(dir).some((f) => f.endsWith(`_${id}.jsonl`));
  } catch {
    return false;
  }
}

// Screen text of first-run dialogs a fresh temp cwd can raise, and the
// keystroke the dialog itself offers to proceed. The probe answers these on
// the CLI's own terms; it never edits a config file. Patterns match the
// screen with ALL whitespace removed (TUIs move the cursor instead of
// printing spaces), hence no spaces below.
const TRUST_PROMPTS = [
  { re: /Doyoutrustthefilesinthisfolder|trustthisfolder|Yes,proceed/i, keys: '\r' },
  { re: /allowCodextoworkinthisfolder|Doyoutrustthecontentsofthisdirectory/i, keys: '\r' },
  // Codex's update nag. Its DEFAULT option runs `curl … | sh`, so this must
  // be Esc ("esc skip") — never Enter. `blocksEnter`: while it is on screen
  // the probe sends nothing else, so no other rule can press Enter into it.
  { re: /Updatenow|Skipuntilnextversion/i, keys: '\x1b', blocksEnter: true },
];

// Exact identities of the injected tool, for the per-run server name the
// probe injects (so a user's own MCP server that happens to be called "hive"
// can never match). Matched against structured fields only (never title/free
// text): see toolIdentity() in probe.mjs.
export const submitIdsFor = (server) =>
  new Set([`mcp__${server}__submit_result`, `${server}__submit_result`, `${server}.submit_result`, `${server}/submit_result`]);

export const AGENTS = {
  claude: {
    route: 'adapter',
    pkg: '@agentclientprotocol/claude-agent-acp',
    version: '0.85.1',
    cli: ['claude', '--version'],
    launch: () => ['npx', '-y', '@agentclientprotocol/claude-agent-acp@0.85.1'],
    cliSessionId: (acpId) => ({ id: acpId, mismatch: false, via: 'identity' }),
    // Mirrors claudeResumeArgs (internal/agent/claude.go:248-253): --resume
    // only when the transcript exists, else --session-id, which silently
    // starts a FRESH session — a takeover failure, not a reopen.
    //
    // Hive's encoder used to diverge from claude's (#494), so this once
    // computed a separate hiveBranch; the two now always agree.
    hiveResume: (id, cwd, home) => {
      const branch = claudeSessionExists(id, cwd, home) ? 'resume' : 'session-id';
      return {
        branch,
        hiveBranch: branch,
        argv: branch === 'resume' ? ['claude', '--resume', id] : ['claude', '--session-id', id],
      };
    },
    headlessResume: (id, prompt) => ['claude', '-p', '--resume', id, prompt],
    trustPrompts: TRUST_PROMPTS,
  },
  codex: {
    route: 'adapter',
    pkg: '@agentclientprotocol/codex-acp',
    version: '2.1.1',
    cli: ['codex', '--version'],
    launch: () => ['npx', '-y', '@agentclientprotocol/codex-acp@2.1.1'],
    cliSessionId: (acpId) => ({ id: acpId, mismatch: false, via: 'identity' }),
    // Mirrors the codex ResumeArgs closure (internal/agent/agent.go:184-186).
    hiveResume: (id) => ({ branch: 'resume', hiveBranch: 'resume', argv: ['codex', 'resume', id] }),
    headlessResume: (id, prompt) => ['codex', 'exec', '--skip-git-repo-check', 'resume', id, prompt],
    // The interactive CLI detaches a managed `codex app-server` daemon
    // (parent pid 1, own process group) that outlives the TUI.
    daemonPattern: /codex app-server/,
    trustPrompts: TRUST_PROMPTS,
  },
  pi: {
    route: 'adapter',
    pkg: 'pi-acp',
    version: '0.0.34',
    cli: ['pi', '--version'],
    launch: () => ['npx', '-y', 'pi-acp@0.0.34'],
    cliSessionId: (acpId, home) => piResolveSessionId(acpId, home),
    // Mirrors the pi ResumeArgs closure (internal/agent/agent.go:242-244):
    // `--session-id` both pins and resumes, so a missing transcript means a
    // fresh session.
    hiveResume: (id, cwd, home) => {
      const branch = piSessionExists(id, cwd, home) ? 'resume' : 'session-id';
      return { branch, hiveBranch: branch, argv: ['pi', '--session-id', id] };
    },
    headlessResume: (id, prompt) => ['pi', '--session-id', id, '-p', prompt],
    trustPrompts: TRUST_PROMPTS,
  },
};
