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

// Mirrors encodeClaudeProjectDir (internal/agent/claude.go:28-34).
export function encodeClaudeProjectDir(cwd) {
  return cwd.replaceAll('/', '-').replaceAll('.', '-').replaceAll(':', '-');
}

// What claude itself writes, observed 2026-10-02 (claude 2.1.288): EVERY
// non-alphanumeric character becomes "-", not just "/", "." and ":". A cwd
// with "_" (macOS $TMPDIR has one) therefore resolves to a different dir
// under Hive's encoder — see the spec 492 design doc's findings.
export function encodeClaudeProjectDirObserved(cwd) {
  return cwd.replace(/[^A-Za-z0-9]/g, '-');
}

const claudeTranscript = (enc, id, cwd, home) =>
  !!id && !!cwd && existsSync(join(home, '.claude', 'projects', enc(cwd), `${id}.jsonl`));

// Mirrors claudeSessionExists (internal/agent/claude.go:45-56).
export function claudeSessionExists(id, cwd, home = homedir()) {
  return claudeTranscript(encodeClaudeProjectDir, id, cwd, home);
}

export function claudeSessionExistsObserved(id, cwd, home = homedir()) {
  return claudeTranscript(encodeClaudeProjectDirObserved, id, cwd, home);
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
    // Mirrors claudeResumeArgs (internal/agent/claude.go:221-226): --resume
    // only when the transcript exists, else --session-id, which silently
    // starts a FRESH session — a takeover failure, not a reopen.
    //
    // hiveBranch is what Hive's code picks today. When it picks --session-id
    // but the transcript DOES exist under claude's real encoding, that is a
    // Hive encoder bug, not an ACP failure: the probe records the bug and
    // still tests the ACP question with --resume.
    hiveResume: (id, cwd, home) => {
      const hiveBranch = claudeSessionExists(id, cwd, home) ? 'resume' : 'session-id';
      const branch = claudeSessionExistsObserved(id, cwd, home) ? 'resume' : 'session-id';
      return {
        branch,
        hiveBranch,
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
