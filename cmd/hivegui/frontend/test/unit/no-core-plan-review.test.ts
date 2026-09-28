// Spec 471: plan review is the bundled plan-review plugin
// (plugins/plan-review), and the app keeps no second implementation of
// its UI. This fails if one creeps back.
//
// Every mention of plan review left in src/ is allowlisted by file and by
// token: the wire field a session carries, the three generic bridge verbs,
// the host's plugin-facing wrappers around them, the SET_CLIENT_UI
// comment, and the core swallowing the plan_review_stale error the plugin
// handles. Anything else — a component, a modal id, a store field, a CSS
// block — is a new token or a new file and fails here.
//
// Sources arrive through Vite's `?raw` glob, not node:fs, for the reason
// test/unit/bridge-harness-parity.test.ts gives.
import { describe, expect, it } from 'vitest';

const sources = import.meta.glob('../../src/**/*.{ts,tsx,css}', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;
const html = import.meta.glob('../../index.html', {
  query: '?raw',
  import: 'default',
  eager: true,
}) as Record<string, string>;

const TOKEN = /[A-Za-z_]*plan[-_ ]?review[A-Za-z_]*/gi;

const ALLOWED: Record<string, string[]> = {
  '../../src/bridge.ts': [
    'GetPlanReview',
    'ResolvePlanReview',
    'GetExternalPlanReviewers',
  ],
  '../../src/app/state.ts': [
    'pending_plan_review',
    'pendingPlanReview',
    'PendingPlanReview',
    'plan-review',
  ],
  '../../src/app/plugin-host.ts': [
    'GetPlanReview',
    'ResolvePlanReview',
    'GetExternalPlanReviewers',
    'getPlanReview',
    'resolvePlanReview',
    'externalPlanReviewers',
    'planreview',
    'plan_review_stale',
    'plan-review',
    'plan review',
    'Plan review',
  ],
  '../../src/app/events.ts': [
    'plan_review_stale',
    'plan-review',
    'plan review',
  ],
};

const DELETED = [
  '../../src/app/modals/plan-review.ts',
  '../../src/components/modals/PlanReview.tsx',
  '../../src/components/PlanReviewBar.tsx',
  '../../src/theme/components/plan-review.css',
];

describe('no plan review UI in the app core (spec 471)', () => {
  it('the core plan-review files are gone', () => {
    expect(Object.keys(sources).length).toBeGreaterThan(50);
    for (const f of DELETED) expect(Object.keys(sources)).not.toContain(f);
  });

  it('mentions plan review only where the allowlist says', () => {
    const stray: string[] = [];
    for (const [file, src] of Object.entries({ ...sources, ...html })) {
      const allowed = new Set(ALLOWED[file] ?? []);
      for (const t of new Set(src.match(TOKEN) ?? [])) {
        if (!allowed.has(t)) stray.push(`${file}: ${t}`);
      }
    }
    expect(stray).toEqual([]);
  });
});
