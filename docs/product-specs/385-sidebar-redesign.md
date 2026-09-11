---
issue: null
title: "Sidebar redesign: readable rows, real worktree groups, density setting"
type: enhancement
complexity: L
priority: P2
stage: RESEARCH
---

# Sidebar redesign: readable rows, real worktree groups, density setting

- **Issue:** — (no tracking issue; number taken as the next free spec number)
- **Type:** enhancement
- **Complexity:** L
- **Stage:** RESEARCH
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/385-sidebar-redesign.md](../exec-plans/active/385-sidebar-redesign.md)
- **Design review:** [docs/design-docs/ui/mocks/sidebar-redesign.html](../design-docs/ui/mocks/sidebar-redesign.html) — the signed-off composite. The rejected options are kept alongside it in `sidebar-redesign-options.html`.

## Problem

The sidebar is the one surface that shows every session at once, and four things get in the way of reading it.

**The agent name is printed twice per row.** `internal/agent/names.go:49` builds an auto-generated name as `adjective-noun <agentID>`, and the row then renders the agent again as a two-letter code in the meta column — `rising-shore claude … cl`.

**Sessions in a worktree share one name, so rows become indistinguishable.** When a session gets a worktree, `internal/registry/create.go:475` names it after the branch: `strings.ReplaceAll(p.wtBranch, "/", "-") + " " + suffix`. Nothing uniquifies the result, so two Claude sessions on `feat/sidebar` are both named `feat-sidebar claude` — byte-identical rows for two different sessions. The information that actually tells them apart, the window title, is on the quiet second line.

**The window title is boxed into a column it does not need to share.** `.hv-session-row__sub` sits in grid column 2, stopping before the idea, worktree, meta and swatch columns, while line 1 is the only line that uses them. The title truncates roughly 70px early for no reason.

**A worktree group does not look like a group.** Sessions sharing a worktree are sorted adjacently and marked with a 3px colour bar on the row's right edge plus a count beside the branch glyph (`session-row.css` `[data-wt-shared]`). Neither reads as containment, and the branch name — the thing that would explain the grouping — is only available in a tooltip.

## Desired behavior

**Rows say each thing once.** The session name drops a trailing agent id when that id is the row's own agent, at display time only; the stored name is untouched, so rename, search and the `hive` CLI are unaffected. A user-chosen name is never altered. The agent is shown as its existing two-letter code on an 18% tint of the agent's own colour (`agent.Def.Color`, which already exists for built-ins and for user-defined agents); an agent with no colour falls back to the plain code on a neutral border.

**The window title gets the whole row.** Line 2 spans from the name column to the row's right edge. Row height is unchanged at 40px — the change is purely horizontal.

**A worktree group is a panel with a header.** Sessions sharing a worktree are wrapped in a bordered, collapsible panel inside the project, headed by the branch name and the member count. Inside a group, a row shows its window title as the primary line and omits the name, because the name is the branch the header already states. A session whose name differs from that branch-derived default — i.e. one the user renamed — keeps showing its name, so a deliberate name is never hidden. Ungrouped sessions stay as plain rows; a single session never forms a group.

**The project is a label, not a card.** The bordered project card is replaced by a small uppercase label with a hairline rule, so the group panel is the only box in the tree and boxing means one thing. The active project is marked by the label taking the accent colour.

**Both headers stay on screen while you scroll.** The project label pins to the top of the list, and a worktree group's header pins directly beneath it, so a group's rows are never on screen without the branch that explains them. Two bars are pinned only while a group is in view; ordinary rows pin one.

**Session colour moves off its own column, and becomes its own control.** The colour that paints the grid tile's border (`theme/layout.css:72,126`) is shown as a 3px full-height bar on the row's right edge instead of a 10px filled chip in its own column. Members of a worktree group share a colour, so the group panel carries one bar for the whole group rather than one per row.

The bar *is* the colour picker: on hover or keyboard focus it widens from 3px to 12px and opens the same OS colour input it opens today. The row reserves that 12px permanently, so the widening costs no reflow and never moves the hover-revealed action buttons. Inside a group the static bar belongs to the group, but the hover target stays per-row, so a click still sets one session's colour.

**Attention is visible from the corner of the eye.** A session wanting attention pulses its row background at up to a 12% tint of `--state-attention` on the existing `--motion-pulse` cadence, degrading to a flat 9% static tint under `prefers-reduced-motion`.

**Density is a setting.** `Settings › Appearance › Sidebar density` offers normal (40px, two lines — the default), tight (~34px, 10.5px subtitle) and compact (28px, one line).

## Success criteria

- A project with three Claude sessions on one worktree shows one panel headed by the branch, and its three rows are told apart by their window titles without the user hovering or switching.
- No row displays its agent's identity more than once.
- A session renamed by the user keeps that name everywhere, inside a worktree group included.
- With the sidebar scrolled into the middle of a long project, the project label is still on screen.
- A session needing attention is noticeable without reading any row, and remains noticeable with animation disabled.
- Switching density to compact shows at least 40% more sessions in the same height than normal does.
- Every session's colour is still discoverable in the sidebar and still matches its grid tile's border, and is still settable from the row by mouse and by keyboard.
- Scrolled to any point inside a worktree group, both the project label and the group's branch header are on screen.
- `biome ci .`, `npm run typecheck`, the DOM tests and the theme snapshots pass.

## Non-goals

- **Renaming the naming scheme.** `agent.RandomName` and the branch-derived name in `create.go` keep producing what they produce today; this spec only changes how a name is displayed.
- **Distinguishing auto-picked colours from user-chosen ones.** `create.go:321` assigns every session a colour via `pickColor()` and nothing records which were deliberate. Hiding "default" colours would need a new `colorExplicit` bit on the entry; the edge-bar treatment sidesteps it by showing every session's colour.
- **Editing window titles.** Hive passes the program's OSC 0/2 title through untouched (`internal/session/session.go:42`) and will continue to. Stripping repeated prefixes from titles is a separate, riskier feature.
- **Grid, tile and minimized-tray surfaces.** Unchanged beyond whatever falls out of shared tokens.
- **New keyboard shortcuts.** ⌘1–⌘9 and ⌘↑/⌘↓ behave as they do today.

## Design decisions carried from review

These were settled against mockups; the exec plan should not relitigate them.

| Question | Decision |
|---|---|
| Agent identity | Two-letter code on an 18% tint of the agent colour; plain code for colourless agents |
| Row layout | Two lines, subtitle spanning `grid-column: 2 / -1`, 40px unchanged |
| Density | Three-value setting; **normal** is the default |
| Worktree group | Bordered panel with branch header; rows show title only, name only when renamed |
| Project header | Flat uppercase label, accent-coloured when active; sticky |
| Session colour | 3px right-edge bar; one bar per group |
| Colour picker | The bar itself — widens to 12px on hover/focus, opens the native input |
| Sticky headers | Project label and group header both pin, group nested under project |
| Attention | Pulsing background tint |

## Open questions

None. Both remaining questions were resolved in review: headers nest, and the colour bar is the picker.

## Notes

Requires an amendment to `docs/design-docs/ui/patterns.md › Selection vs attention`, which currently reserves the row background for selection and forbids attention from using it. Proposed replacement: selection owns the row's *static* background and the left edge; attention never uses the left edge and never paints a static background, but may pulse. Under `prefers-reduced-motion` both become static, so their alphas must stay ordered — attention at 9% below `--sel` — and that ordering is worth an assertion in the theme tests rather than a comment.
