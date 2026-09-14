---
type: fixed
bump: patch
---

A session that had printed more than 8 MiB of output got slower and slower to write
to — permanently. The raw scrollback ring trimmed itself by allocating a new buffer
at exactly the retained size and copying the whole thing across, so every later PTY
read re-copied the full 8 MiB and threw away ~19 MB of garbage: about 1.7 ms of CPU
per read. On Windows, where ConPTY hands back roughly one line at a time, that cost
was paid per line of output and capped a busy session at tens of KB/s.

The ring is now a fixed circular buffer allocated once, so trimming is a pointer
move: no copying, no allocation, and the same bytes come back on reattach. Writes
past the cap are roughly a thousand times faster, and steady-state scrollback churn
allocates nothing at all.
