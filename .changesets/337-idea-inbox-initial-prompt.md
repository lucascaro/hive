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
  Pi receive it as their opening argument; every other agent has it
  typed into the terminal as soon as the session settles — left in the
  input box for you to send, rather than submitted for you, so it can
  never answer an agent's own startup prompt on your behalf. The idea then flips to
  *started* and links back to the session, and that session's sidebar
  row carries a small idea glyph so you can see at a glance what it is
  for. Agents that cannot be handed an opening prompt — the plain shell,
  and your own custom agents — say so in the launcher before you start,
  and leave the idea in the inbox rather than marking it started.
- **A mis-filed idea can be corrected.** The inbox's Edit now opens the
  same sheet you captured it with, so the note, its kind *and* the
  project it belongs to are all editable — the capture sheet fills the
  project in from whatever session you happened to be in, so getting it
  wrong is ordinary, and deleting and retyping was not a fix.
