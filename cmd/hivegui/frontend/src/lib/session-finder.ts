// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Session finder (spec 544): matching and ordering for the "go to session"
// modal. Pure — no DOM, no store — so it is unit-testable and the modal is
// a thin view over it (same idiom as lib/nav-history.ts).
//
// Every call derives the rows from the sessions/projects it is handed, so
// a rename or a changed branch is matched by its new value with no cache
// to invalidate.

// The slice of SessionInfo / ProjectInfo the finder reads. Structural, so
// this module imports nothing from app/. Wire fields are dual-spelled
// (snake_case on the wire, camelCase through some bridges); `title` is
// snake-only (SessionInfo.title).
export interface FinderSession {
  id: string;
  name?: string;
  title?: string;
  agent?: string;
  project_id?: string;
  projectId?: string;
  worktree_branch?: string;
  worktreeBranch?: string;
}

export interface FinderProject {
  id: string;
  name?: string;
}

export interface FinderRow {
  id: string;
  name: string;
  title: string;
  project: string;
  branch: string;
  agent: string;
}

const SEPARATORS = new Set(['-', '_', '/', '.', ' ', ':']);
const CONSECUTIVE = 5;
const WORD_START = 4;
const PREFIX = 6;

// scoreFrom is one greedy left-to-right subsequence match starting with
// the first query char at `start`. Higher is better; null is no match.
function scoreFrom(q: string, t: string, start: number): number | null {
  let score = 0;
  let prev = -2;
  let ti = start;
  for (let qi = 0; qi < q.length; qi++) {
    const at = qi === 0 ? start : t.indexOf(q[qi], ti);
    if (at < 0) return null;
    score += 1;
    if (at === prev + 1) score += CONSECUTIVE;
    else if (prev >= 0) score -= Math.min(at - prev - 1, 3);
    if (at === 0) score += WORD_START + PREFIX;
    else if (SEPARATORS.has(t[at - 1])) score += WORD_START;
    prev = at;
    ti = at + 1;
  }
  return score;
}

// fuzzyScore: case-insensitive subsequence match of `query` in `text`,
// preferring contiguous runs, word-start hits and prefixes. null when it
// does not match (an empty or missing text never matches a non-empty
// query; an empty query matches everything with score 0).
export function fuzzyScore(
  query: string,
  text: string | undefined,
): number | null {
  const q = query.toLowerCase();
  if (q === '') return 0;
  const t = (text ?? '').toLowerCase();
  let best: number | null = null;
  for (let i = t.indexOf(q[0]); i >= 0; i = t.indexOf(q[0], i + 1)) {
    const s = scoreFrom(q, t, i);
    if (s !== null && (best === null || s > best)) best = s;
  }
  return best;
}

export interface FinderInput {
  sessions: readonly FinderSession[];
  projects: readonly FinderProject[];
  // state.nav.back: ids of DEPARTED sessions, most recent last.
  back: readonly string[];
  activeId: string | null;
  query: string;
}

// recencyOrder: the previously focused session first, the active one
// last, everything else in the order given. Ids no longer present are
// skipped and repeats collapse. Derived from the nav history rather than
// a dedicated MRU, so a ⌃- walk makes it approximate — good enough for
// "chord, Enter = back to the last session".
function recencyOrder(
  sessions: readonly FinderSession[],
  back: readonly string[],
  activeId: string | null,
): FinderSession[] {
  const byId = new Map(sessions.map((s) => [s.id, s]));
  const out: FinderSession[] = [];
  const seen = new Set<string>();
  const take = (s: FinderSession | undefined) => {
    if (!s || seen.has(s.id)) return;
    seen.add(s.id);
    out.push(s);
  };
  seen.add(activeId ?? '');
  for (let i = back.length - 1; i >= 0; i--) take(byId.get(back[i]));
  for (const s of sessions) take(s);
  if (activeId) {
    const active = byId.get(activeId);
    if (active) out.push(active);
  }
  return out;
}

export function finderRows(input: FinderInput): FinderRow[] {
  const projectName = new Map(input.projects.map((p) => [p.id, p.name ?? '']));
  const rows = recencyOrder(input.sessions, input.back, input.activeId).map(
    (s) => ({
      id: s.id,
      name: s.name ?? '',
      title: s.title ?? '',
      project: projectName.get(s.projectId ?? s.project_id ?? '') ?? '',
      branch: s.worktree_branch ?? s.worktreeBranch ?? '',
      agent: s.agent ?? '',
    }),
  );
  const terms = input.query.split(/\s+/).filter(Boolean);
  if (terms.length === 0) return rows;
  const scored: { row: FinderRow; score: number }[] = [];
  for (const row of rows) {
    let total = 0;
    let ok = true;
    for (const term of terms) {
      let best: number | null = null;
      for (const field of [row.name, row.title, row.branch, row.project]) {
        const s = fuzzyScore(term, field);
        if (s !== null && (best === null || s > best)) best = s;
      }
      if (best === null) {
        ok = false;
        break;
      }
      total += best;
    }
    if (ok) scored.push({ row, score: total });
  }
  // Array.prototype.sort is stable: ties keep the recency order above.
  return scored.sort((a, b) => b.score - a.score).map((x) => x.row);
}
