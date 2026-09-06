// ---------- the What's New list: parsing, grouping, and "have I read it" ----------
//
// Source of truth is site/features.json — the same curated, user-facing list
// the website renders. Not CHANGELOG.md: that file is generated from
// .changesets/ and is written for people reading the repo, where this list is
// written for people using the app. One list, two renderers (site/build.mjs
// and components/modals/WhatsNew.tsx), so the two cannot drift.
//
// Imported rather than fetched: the list is bundled into the build, which is
// what makes the modal work offline AND makes the "unread" question answerable
// without asking the daemon anything — see hasUnread.
//
// Pure and DOM-free on purpose, the way lib/collapsed.ts is: the localStorage
// read and write live at the call site, in app/modals/whats-new.ts.
import features from '../../../../../site/features.json';

export interface Feature {
  title: string;
  blurb?: string;
  status: string;
  /** Release this shipped in. Required for `shipped`, absent for `planned`. */
  since?: string;
  highlight?: boolean;
}

export interface VersionGroup {
  version: string;
  entries: Feature[];
}

/** The bundled list. Exported so the unit test can assert against real data. */
export const FEATURES: Feature[] = features;

export const SEEN_KEY = 'hive.whatsNewSeen';

// Numeric-aware, because a string sort puts 2.10.0 before 2.9.0 and sorts
// "2.0.0-alpha.2" after "2.0.0". Returns <0 / 0 / >0 like any comparator.
// A pre-release sorts BEFORE its release (2.0.0-alpha.2 < 2.0.0), which is
// both the semver rule and the order that reads right in the modal.
export function compareVersions(a: string, b: string): number {
  const split = (v: string) => {
    const [core, pre] = v.split('-', 2);
    return {
      nums: core.split('.').map((n) => Number.parseInt(n, 10) || 0),
      pre,
    };
  };
  const x = split(a);
  const y = split(b);
  for (let i = 0; i < Math.max(x.nums.length, y.nums.length); i++) {
    const d = (x.nums[i] ?? 0) - (y.nums[i] ?? 0);
    if (d !== 0) return d;
  }
  // Same core: having no pre-release outranks any pre-release.
  if (x.pre === y.pre) return 0;
  if (x.pre === undefined) return 1;
  if (y.pre === undefined) return -1;
  return x.pre < y.pre ? -1 : 1;
}

/** Shipped features bucketed by `since`, newest version first. */
export function groupByVersion(list: Feature[] = FEATURES): VersionGroup[] {
  const buckets = new Map<string, Feature[]>();
  for (const f of list) {
    if (f.status !== 'shipped' || !f.since) continue;
    const bucket = buckets.get(f.since);
    if (bucket) bucket.push(f);
    else buckets.set(f.since, [f]);
  }
  return [...buckets.entries()]
    .map(([version, entries]) => ({ version, entries }))
    .sort((p, q) => compareVersions(q.version, p.version));
}

/** Everything still to come, in file order. */
export function plannedOf(list: Feature[] = FEATURES): Feature[] {
  return list.filter((f) => f.status === 'planned');
}

/**
 * The newest version in the bundled list — this build's frontier.
 *
 * Deliberately NOT the running app's version, which arrives at runtime over
 * the Wails "daemon:stale" event (components/VersionFooter.tsx). That is the
 * wrong clock: the list ships inside the bundle, so the highest `since` in it
 * is by definition the newest thing this build can tell the user about.
 */
export function latestVersion(list: Feature[] = FEATURES): string | null {
  return groupByVersion(list)[0]?.version ?? null;
}

/**
 * Whether the gift should carry its dot.
 *
 * A missing `seen` counts as unread on purpose: the users who most need to
 * find this modal are the ones updating INTO the release that adds it, and
 * they have nothing stored. An unparseable value reads as unread exactly
 * once, because opening the modal rewrites the key.
 */
export function hasUnread(latest: string | null, seen: string | null): boolean {
  if (!latest) return false;
  if (!seen || !/^\d+\.\d+\.\d+/.test(seen)) return true;
  return compareVersions(latest, seen) > 0;
}
