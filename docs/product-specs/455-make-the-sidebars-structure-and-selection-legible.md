---
issue: 455
title: Make the sidebar's structure and selection legible
type: enhancement
complexity: M
priority: P2
stage: IMPLEMENT
---

# Make the sidebar's structure and selection legible

- **Issue:** #455
- **Exec plan:** —

## Problem

In the GUI sidebar, three things are hard to see. Worktree groups barely differ from ungrouped sessions: the group background (`--surface-raised`) is about 1% lighter than the sidebar background, so two hairlines are the only signal. The selected session's `--sel` background is close to the hover background and to the sidebar background, so it is hard to find at a glance. Project headers fade between session rows. The 8px right gutter on the session list also looks like wasted space, because the left edge has none.

## Desired behavior

- Worktree groups: members are indented about 13px behind a 1px rail in the group's colour. The group header's branch and label text take the group colour. The raised background goes. The hairlines, the ~4px gap around the group and the per-row colour bars stay.
- Selected row: filled with the accent colour (`--accent`; 88% was tuned first but fails AA on light presets), with `--on-accent` text, clearly different from hover. A selected session that needs attention still shows an attention cue that stays distinct from the fill.
- List edges: no gutter on either side. The session colour bar stays at the row's right edge. The sidebar resize handle sits just outside the sidebar's edge instead of inside it, so the colour bar and its picker stay fully clickable.
- Project headers: a 2px top line in the project's colour, with a 4px margin above each project (the visible gap between projects is about 16px including the previous project's bottom margin).
- All of this works on every theme preset and at every density.

## Success criteria

1. On every preset, a worktree group's member rows are indented behind a rail in the group colour, and its header text takes the group colour. Ungrouped rows have no rail and no indent.
2. On every preset, the selected row's background is the accent fill. Its text passes WCAG AA against that fill, and it looks clearly different from a hovered row.
3. A selected row whose session needs attention shows an attention cue that stays distinct from the accent fill on every preset, including hive-dark, where `--accent` and `--state-attention` are close in hue. `patterns.md` › Selection vs attention is updated to describe the new selection and attention channels.
4. The session list has 0px padding on both sides.
5. The resize handle sits just outside the sidebar's edge. Each row's colour picker opens on click and on keyboard focus without triggering a resize, and dragging the border still resizes the sidebar.
6. Each project header shows a 2px top line in the project's colour.
7. `docs/design-docs/ui/` (components.md and patterns.md) describes the new visuals, and `scripts/ui-lint.sh` and the ui-contrast gate pass.

## Non-goals

- The "checkout" label for ungrouped sessions, putting ungrouped sessions first, the branch glyph, dimming unselected rows, and the ancestor marker on headers. All were considered and turned down in the design tuner.
- Changes to session ordering, collapse behaviour, keybindings or row content.
- New theme presets, or token values beyond what the new styles need.
- The grid view and tile chrome.

## Notes

- The design came from an interactive tuner (two rounds). Final raw state: `{"group":"rail","g-gap":4,"hair":true,"rail-w":1,"rail-indent":13,"railc":"session","railbar":"on","railhead":"colour","sel":"filled","fill":88,"filltext":"on-accent","hover-mix":60,"edge":"flush-right","resizer":"straddle","cb-w":3,"ph":"rule","ph-h":26,"sw":8,"proj-gap":4}`
- Open: which attention cue on a filled selected row (a ring or bar in `--state-attention` is low-contrast against the fill on hive-dark). Decide during planning.
- Open: hover still uses `--sel` at 60%. With the filled selection it may need retuning. The operator kept 60% in the tuner.
- Open: header text in the group colour must meet contrast on light presets (colour is user data, not a token, so ui-contrast cannot check it).
