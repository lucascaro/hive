---
issue: null
pr: 377
type: added
bump: minor
---
- **Start a session from an idea.** Every open note in the inbox gets a
  **Start session** action: it opens the usual agent launcher with the
  project pinned and an opening prompt built from the note, worktree
  checkbox and all. The prompt is an instruction, not a label — a bug
  asks the agent to reproduce it and find the root cause before
  touching code, an idea asks it to propose a plan first — with your
  note as the subject — and editable right there in the launcher, since
  the note was jotted down mid-task and this is the last moment to
  sharpen it (sharpening it does not change the stored note). Claude and
  Pi receive it as their opening argument, and for them the idea flips to *started* and links back to its session,
  whose sidebar row carries a small idea glyph. Every other agent gets
  the prompt **offered** instead of typed in: a bar above the grid
  shows the note with **Paste** and **Dismiss**, so you place it once
  you can see the agent is actually ready — Hive cannot tell an agent's
  input box from its startup "do you trust this directory?" gate, and
  you can. Paste drops it in without sending it and links the idea;
  Dismiss leaves the note in the inbox. The plain shell and your own
  custom agents get no prompt at all, and the launcher says so before
  you start.
- **A mis-filed idea can be corrected.** The inbox's Edit now opens the
  same sheet you captured it with, so the note, its kind *and* the
  project it belongs to are all editable — the capture sheet fills the
  project in from whatever session you happened to be in, so getting it
  wrong is ordinary, and deleting and retyping was not a fix.
