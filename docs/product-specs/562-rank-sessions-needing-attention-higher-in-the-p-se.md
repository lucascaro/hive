---
issue: 562
title: "Rank sessions needing attention higher in the ⌘P session finder"
type: enhancement
complexity: S
priority: P2
stage: IMPLEMENT
---

# Rank sessions needing attention higher in the ⌘P session finder

- **Issue:** #562
- **Type:** enhancement
- **Complexity:** S
- **Priority:** P2
- **Exec plan:** [docs/exec-plans/active/562-rank-sessions-needing-attention-higher-in-the-p-se.md](../exec-plans/active/562-rank-sessions-needing-attention-higher-in-the-p-se.md)

## Problem

With many sessions open, a session waiting on the user (input, permission, or a failure) often sits far down the ⌘P finder, because the list is ordered only by recency. ⌘B can jump to flagged sessions, but it cycles through them blindly. In the finder, which is where the user is already looking for a session, flagged sessions have no priority and must be found by scrolling or by remembering a name.

## Desired behavior

On an empty query, the ⌘P finder lists:
1. The previously focused session, unchanged, so ⌘P then Enter still goes back.
2. Every session in the `attention`, `waiting-permission`, `failed` or `error` state, in recency order.
3. All other sessions, in recency order.
4. The current session, last, as today.

While typing, the match score still decides the order. Among rows with equal scores, a flagged session ranks ahead of one that isn't flagged, and recency breaks any remaining tie. The order follows live state: a session that becomes flagged, or stops being flagged, moves into or out of the group without restarting the app.

## Success criteria

1. With an empty query, the previously focused session is first, even when other sessions are flagged and it isn't. Spec 544 criterion 3 still passes.
2. With an empty query, every session in `attention`, `waiting-permission`, `failed` or `error` appears ahead of every unflagged session other than the previous and current ones. Unit-tested, one case per state.
3. Within the flagged group and within the unflagged group, the order matches the existing recency order.
4. With a typed query, a better-scoring unflagged match still ranks above a worse-scoring flagged match. At equal score, the flagged session ranks first.
5. The current session stays last regardless of its state.
6. A session whose flag clears while the finder is closed is ranked as unflagged the next time ⌘P opens.
7. The open-and-filter time with 100 mock sessions stays under 100 ms (spec 544 criterion 7 still passes).

## Non-goals

- Any change to ⌘B / ⇧⌘B: their cycle order, the states they consider, and how they jump back.
- An attention-only filter, toggle, or query prefix.
- Clearing or acknowledging a flag on opening the finder or moving the selection. Only focusing the session clears it, as today.
- Ordering flagged sessions by severity or by how long they have waited.
- The command palette's command mode.

## Notes

Open questions:
- Should flagged rows get extra visual treatment (a section header or highlight), or is the existing status icon enough? The operator did not mark this as a non-goal.
- Tie-break only: integer fuzzy scores tie often on short queries and rarely on longer ones, so attention mostly reorders the empty-query list and the first few keystrokes. The operator accepted this.

Related: spec 544 (session finder, `lib/session-finder.ts`), spec 240 (⌘B jump to attention).
