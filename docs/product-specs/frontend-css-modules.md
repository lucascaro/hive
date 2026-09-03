---
issue: null
title: "Migrate the frontend to CSS Modules"
type: enhancement
complexity: L
priority: P3
stage: TRIAGE
---

# Migrate the frontend to CSS Modules

- **Issue:** —
- **Type:** enhancement
- **Complexity:** L
- **Priority:** P3
- **Exec plan:** —

## Problem

Frontend styling is global CSS keyed on hand-written `hv-*` BEM classes and data
attributes (`src/theme/tokens.css`, `themes.css`, `base.css`, `layout.css`,
`components/*.css`). Nothing scopes a component's styles to that component, so a
class name is a global identifier and a rename is a repo-wide grep.

## Desired behaviour

Component styles are colocated and scoped; the token layer
(`tokens.css` / `themes.css`) stays global, because tokens are the API.

## The blocking constraint, and step 1

The `hv-*` classes are not only the CSS contract — they are the **e2e selector
contract**. There are **zero `data-testid` attributes in the repo**, by an
explicit decision: the 31 Playwright specs select on ids, `hv-*` classes and
data attributes, which is what made them the safety proof that carried the React
rewrite through six phases without a spec edit. CSS Modules hash those class
names, so every one of those selectors breaks at once.

**So step 1 of this work is the e2e testid/selector strategy, and it ships on
its own, before any CSS moves.** Decide and land: which stable hook the specs
select on (a `data-testid`, a preserved semantic data attribute, or a
non-hashed class prefix), applied across all 30 specs, green, merged. Only then
is the styling migration a mechanical, per-component job.

## Non-goals

- No Tailwind. Considered and rejected during the React rewrite: it breaks every
  e2e selector and the ui-lint conventions for no migration benefit.
- No change to the token layer or the theme presets.

## Success criteria

- Step 1 merged separately: every e2e spec selects on a hook that survives class
  hashing, with no behaviour change.
- Component CSS is colocated and scoped; `scripts/ui-lint.sh --strict` still
  passes (its raw-hex / px-font / Unicode-icon rules are orthogonal to scoping
  and must keep applying).
- No visual change: the presets and the WCAG contrast gate are unaffected.

## Context

Filed by Phase 6 of the React UI rewrite ([spec](react-ui-rewrite.md)) as a
named debt item.
