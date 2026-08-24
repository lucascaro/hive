// Pure logic for the worktree browser: how a worktree row is
// classified, whether it can be deleted, and how the two lists sort.
// DOM-free so it can be unit-tested — the modal in
// app/modals/worktrees.ts renders whatever this decides.
//
// Wire payloads (internal/wire/control.go) are snake_case; the older
// GUI paths also carry camelCase, so both spellings are read at the
// boundary, matching lib/wire.ts.

export interface WorktreeInfo {
  path: string;
  branch?: string;
  detached?: boolean;
  is_main?: boolean;
  isMain?: boolean;
  uncommitted?: boolean;
  unpushed?: number;
  unknown?: boolean;
  merged?: boolean;
  // Tracking ref ("origin/foo"), absent when the branch tracks
  // nothing — and so when there is no remote branch to delete.
  upstream?: string;
  session_ids?: string[];
  sessionIds?: string[];
  // First line of the branch tip's commit message.
  subject?: string;
}

export interface BranchInfo {
  name: string;
  upstream?: string;
  ahead?: number;
  merged?: boolean;
  // First line of the branch tip's commit message.
  subject?: string;
}

export interface WorktreesPayload {
  project_id?: string;
  projectId?: string;
  repo_root?: string;
  repoRoot?: string;
  worktrees?: WorktreeInfo[];
  orphan_branches?: BranchInfo[];
  orphanBranches?: BranchInfo[];
}

export function readIsMain(w: WorktreeInfo): boolean {
  return !!(w.is_main ?? w.isMain);
}

export function readSessionIds(w: WorktreeInfo): string[] {
  return w.session_ids ?? w.sessionIds ?? [];
}

export function readRepoRoot(p: WorktreesPayload): string {
  return p.repo_root ?? p.repoRoot ?? '';
}

export function readProjectIdOf(p: WorktreesPayload): string {
  return p.project_id ?? p.projectId ?? '';
}

export function readWorktrees(p: WorktreesPayload): WorktreeInfo[] {
  return p.worktrees ?? [];
}

export function readOrphanBranches(p: WorktreesPayload): BranchInfo[] {
  return p.orphan_branches ?? p.orphanBranches ?? [];
}

// How a row reads at a glance.
//   main     — the project's own checkout; never removable
//   active   — a session is running in it
//   holding  — detached, but carries work (uncommitted, unpushed, or
//              an unanswerable base). Deleting it loses something.
//   idle     — detached and provably empty; safe to sweep
//
// `merged` (the branch is already in the default ref, squash merges
// included) neutralises the unpushed count: those commits exist
// upstream, so the row is disposable despite being ahead.
export type WorktreeKind = 'main' | 'active' | 'holding' | 'idle';

export function classifyWorktree(w: WorktreeInfo): WorktreeKind {
  if (readIsMain(w)) return 'main';
  if (readSessionIds(w).length > 0) return 'active';
  if (w.uncommitted || hasUnpushedWork(w) || w.unknown) return 'holding';
  return 'idle';
}

// Unpushed commits that are not already merged elsewhere — the only
// kind whose loss matters.
function hasUnpushedWork(w: WorktreeInfo): boolean {
  return (w.unpushed ?? 0) > 0 && !w.merged;
}

// A single blocker: `absolute` ones cannot be overridden by force, so
// the UI disables the action instead of offering a confirm.
export interface Blocker {
  reason: string;
  absolute: boolean;
}

// deleteBlockers returns what stands between the user and deleting
// this worktree, most severe first. Empty means it can go with no
// confirm at all.
//
// Order matters and mirrors the daemon's refusal order
// (registry.RemoveWorktree), so the message the user reads is the one
// the daemon would actually send back.
export function deleteBlockers(w: WorktreeInfo): Blocker[] {
  const out: Blocker[] = [];
  if (readIsMain(w)) {
    out.push({
      reason: 'This is the project’s main checkout.',
      absolute: true,
    });
    // Nothing else is worth saying about a row that can never go.
    return out;
  }
  const sessions = readSessionIds(w);
  if (sessions.length > 0) {
    out.push({
      reason:
        sessions.length === 1
          ? '1 session is running in it. Close it first.'
          : `${sessions.length} sessions are running in it. Close them first.`,
      absolute: true,
    });
  }
  if (w.uncommitted) {
    out.push({ reason: 'It has uncommitted changes.', absolute: false });
  }
  const unpushed = w.unpushed ?? 0;
  if (hasUnpushedWork(w)) {
    out.push({
      reason:
        unpushed === 1
          ? 'It has 1 commit that is not pushed.'
          : `It has ${unpushed} commits that are not pushed.`,
      absolute: false,
    });
  } else if (w.unknown) {
    // No upstream and no default ref: we genuinely cannot tell
    // whether there is unpushed work. Say so rather than implying
    // it's clean.
    out.push({
      reason: 'Its commits could not be compared to any remote.',
      absolute: false,
    });
  }
  return out;
}

// canDelete is false only for the absolute blockers; everything else
// is deletable behind a confirm.
export function canDelete(w: WorktreeInfo): boolean {
  return !deleteBlockers(w).some((b) => b.absolute);
}

// needsConfirm reports whether deleting requires the force path.
export function needsConfirm(w: WorktreeInfo): boolean {
  return deleteBlockers(w).length > 0;
}

// Renaming moves the directory, so it is refused for exactly the
// reasons a session would be left in a missing cwd — plus the two
// cases with no branch to rename.
export function canRename(w: WorktreeInfo): boolean {
  return (
    !readIsMain(w) &&
    readSessionIds(w).length === 0 &&
    !w.detached &&
    !!w.branch
  );
}

// One-line status for the row, e.g. "2 sessions · uncommitted changes".
export function statusLabel(w: WorktreeInfo): string {
  const parts: string[] = [];
  if (readIsMain(w)) parts.push('main checkout');
  const n = readSessionIds(w).length;
  if (n === 1) parts.push('1 session');
  else if (n > 1) parts.push(`${n} sessions`);
  if (w.detached) parts.push('detached HEAD');
  if (w.uncommitted) parts.push('uncommitted changes');
  // Merged shows as a badge on the row, not as another word here —
  // but it still suppresses the ahead count, which is noise once the
  // work is upstream.
  const unpushed = w.unpushed ?? 0;
  if (hasUnpushedWork(w)) {
    parts.push(
      unpushed === 1 ? '1 unpushed commit' : `${unpushed} unpushed commits`,
    );
  } else if (w.unknown && !readIsMain(w)) {
    parts.push('no remote to compare');
  }
  if (parts.length === 0) parts.push('clean');
  return parts.join(' · ');
}

// Sort order: main first (it is the anchor), then active worktrees,
// then the ones holding work, then the disposable ones — the list
// reads top-to-bottom from "in use" to "safe to sweep". Ties break on
// branch, then path, so the order is stable across refreshes.
const KIND_RANK: Record<WorktreeKind, number> = {
  main: 0,
  active: 1,
  holding: 2,
  idle: 3,
};

export function sortWorktrees(list: WorktreeInfo[]): WorktreeInfo[] {
  return [...list].sort((a, b) => {
    const d = KIND_RANK[classifyWorktree(a)] - KIND_RANK[classifyWorktree(b)];
    if (d !== 0) return d;
    const ab = a.branch ?? '';
    const bb = b.branch ?? '';
    if (ab !== bb) return ab.localeCompare(bb);
    return a.path.localeCompare(b.path);
  });
}

// Orphan branches sort unmerged-first — those are the ones carrying
// work someone may want back — then alphabetically.
export function sortBranches(list: BranchInfo[]): BranchInfo[] {
  return [...list].sort((a, b) => {
    if (!!a.merged !== !!b.merged) return a.merged ? 1 : -1;
    return a.name.localeCompare(b.name);
  });
}

export function branchStatusLabel(b: BranchInfo): string {
  const parts: string[] = [];
  // "merged" is the row's badge, not a word here.
  const ahead = b.ahead ?? 0;
  if (ahead > 0) {
    parts.push(ahead === 1 ? '1 commit ahead' : `${ahead} commits ahead`);
  }
  if (b.upstream) parts.push(`tracks ${b.upstream}`);
  if (parts.length === 0) parts.push('no worktree');
  return parts.join(' · ');
}

// The last path segment, which is what the user recognises — the full
// path goes in the row's title attribute.
export function shortPath(path: string): string {
  const parts = path.split('/').filter(Boolean);
  return parts.length ? parts[parts.length - 1] : path;
}

// Text for the delete confirmation. Spelled out in full because this
// is the destructive step: it names what is lost and, when the branch
// goes too, says so explicitly.
export function deleteConfirmMessage(
  w: WorktreeInfo,
  deleteBranch: boolean,
): string {
  const lines: string[] = [`Delete the worktree at ${w.path}?`];
  const blockers = deleteBlockers(w);
  if (blockers.length > 0) {
    lines.push('');
    for (const b of blockers) lines.push(`• ${b.reason}`);
  }
  lines.push('');
  if (deleteBranch && w.branch) {
    lines.push(
      `The directory and the branch “${w.branch}” will both be deleted. This cannot be undone.`,
    );
  } else if (w.branch) {
    lines.push(
      `The directory will be deleted. The branch “${w.branch}” is kept, so the commits on it can still be recovered.`,
    );
  } else {
    lines.push('The directory will be deleted. This cannot be undone.');
  }
  return lines.join('\n');
}
