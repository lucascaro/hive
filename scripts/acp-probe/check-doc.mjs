#!/usr/bin/env node
// Keeps docs/design-docs/acp-workflows.md honest against the probe results
// (spec 492). Fails when:
//   - the capability table is not 5 agent rows × 7 data columns,
//   - a cell is not tagged [run] or [doc],
//   - a probed agent (claude, codex, pi) has no results file, or any of its
//     cells is tagged [doc],
//   - a [run] cell disagrees with the agent's latest results/*.json,
//   - the doc's go/no-go verdict differs from the one computed from results.
//
//   node scripts/acp-probe/check-doc.mjs <doc.md> [--results <dir>]
import { readdirSync, readFileSync, realpathSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

export const AGENT_ROWS = ['claude', 'codex', 'gemini', 'copilot', 'pi'];
export const PROBED = ['claude', 'codex', 'pi'];
// Data columns after "Agent", in order; null = the route column.
export const COLUMNS = [null, 'mcp', 'permission', 'streaming', 'load', 'takeover_headless', 'takeover_pty'];

export function parseTable(md) {
  const m = /<!-- capability-table:start -->([\s\S]*?)<!-- capability-table:end -->/.exec(md);
  if (!m) return null;
  const rows = m[1].split('\n').filter((l) => l.trim().startsWith('|'));
  return rows.slice(2).map((l) => l.trim().replace(/^\||\|$/g, '').split('|').map((c) => c.trim()));
}

// SC6 — the one rule. For claude and codex: ok = mcp pass && takeover_pty pass.
//   go       — at least one is ok
//   no-go    — mcp fails for both, or takeover_pty fails for both
//   undecided — anything else (including all-inconclusive); never go.
export function computeVerdict(results) {
  const pair = ['claude', 'codex'].map((a) => results[a]?.checks ?? {});
  const v = (c, k) => c[k]?.verdict;
  if (pair.some((c) => v(c, 'mcp') === 'pass' && v(c, 'takeover_pty') === 'pass')) return 'go';
  if (pair.every((c) => v(c, 'mcp') === 'fail') || pair.every((c) => v(c, 'takeover_pty') === 'fail')) return 'no-go';
  return 'undecided';
}

export function checkDoc(md, results) {
  const errors = [];
  const rows = parseTable(md);
  if (!rows) return ['no <!-- capability-table:start/end --> block'];
  const byAgent = new Map(rows.map((r) => [r[0]?.toLowerCase().split(/\s+/)[0], r]));
  for (const agent of AGENT_ROWS) {
    const row = byAgent.get(agent);
    if (!row) { errors.push(`${agent}: missing row`); continue; }
    if (row.length !== COLUMNS.length + 1) { errors.push(`${agent}: ${row.length - 1} data columns, want ${COLUMNS.length}`); continue; }
    const res = results[agent];
    if (PROBED.includes(agent) && !res) errors.push(`${agent}: probed agent has no results file`);
    COLUMNS.forEach((key, i) => {
      const cell = row[i + 1];
      const tag = /\[(run|doc)\]/.exec(cell)?.[1];
      const where = `${agent}/${key ?? 'route'}`;
      if (!tag) { errors.push(`${where}: cell not tagged [run] or [doc]`); return; }
      if (PROBED.includes(agent) && tag !== 'run') { errors.push(`${where}: probed agent cell tagged [doc]`); return; }
      if (tag !== 'run' || !res) return;
      if (key === null) {
        if (!res.adapter || !cell.includes(res.adapter)) errors.push(`${where}: route cell does not name ${res.adapter}`);
        return;
      }
      const want = res.checks?.[key]?.verdict;
      if (want === undefined) { errors.push(`${where}: results file has no verdict for ${key}`); return; }
      const got = /^[`*]*([a-z-]+)/.exec(cell)?.[1];
      if (got !== want) errors.push(`${where}: doc says ${got}, results say ${want}`);
    });
  }
  const docVerdict = /\*\*Verdict:\s*(go|no-go|undecided)\*\*/.exec(md)?.[1];
  const want = computeVerdict(results);
  if (!docVerdict) errors.push('no **Verdict: go|no-go|undecided** line');
  else if (docVerdict !== want) errors.push(`go/no-go: doc says ${docVerdict}, results compute ${want}`);
  return errors;
}

// Latest results file per agent (names sort by date: <agent>-YYYY-MM-DD.json).
export function loadResults(dir) {
  const out = {};
  for (const f of readdirSync(dir).filter((x) => x.endsWith('.json')).sort()) {
    const agent = f.replace(/-\d{4}-\d{2}-\d{2}\.json$/, '');
    out[agent] = JSON.parse(readFileSync(join(dir, f), 'utf8'));
  }
  return out;
}

if (process.argv[1] && realpathSync(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const doc = process.argv[2];
  const ri = process.argv.indexOf('--results');
  const dir = ri > 0 ? process.argv[ri + 1] : join(dirname(fileURLToPath(import.meta.url)), 'results');
  if (!doc) {
    process.stderr.write('usage: check-doc.mjs <doc.md> [--results <dir>]\n');
    process.exit(2);
  }
  const errors = checkDoc(readFileSync(doc, 'utf8'), loadResults(dir));
  for (const e of errors) process.stderr.write(`check-doc: ${e}\n`);
  if (errors.length) process.exit(1);
  process.stdout.write(`check-doc: ${doc} matches ${dir}\n`);
}
