---
type: fixed
bump: patch
pr: 391
---

Pasting, mouse tracking and arrow keys keep working after a session is
reattached or the layout changes width. Repainting a tile begins with a soft
reset, which clears the DEC private modes the running program set — and the
program never sends them again, because it has no idea a new client attached.
Only the cursor and alt-screen were being restored, so everything else was lost
for the life of that tile. Bracketed paste was the visible casualty: without it
agents guess where a paste starts and ends, and anything over about 1 KiB
arrived as several separate pastes, because that is where the operating system
splits a write to the terminal. The daemon now tracks those modes and re-asserts
whatever was live, both in the reattach snapshot and in the resize replay — and
turns off the ones the program switched off while nobody was attached, so a
finished TUI no longer leaves mouse tracking on and spraying click codes into
the shell that follows it.
