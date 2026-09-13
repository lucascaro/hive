---
type: fixed
bump: patch
---

Hive no longer gets quietly demoted to Windows' efficiency mode. Windows watches
for processes that are not the foreground window and moves them to EcoQoS —
parked on the efficiency cores, clocked down — and Hive fits that description by
design: you spend your time typing into the terminal, not into chrome around it.
Nothing had ever told Windows otherwise, so the scheduler was free to decide, and
on a machine with performance and efficiency cores it often decided wrong. The
process now opts out of execution-speed throttling at startup, which is what
macOS has done all along via App Nap. On older Windows, where the API predates
the setting, Hive notes it in the log and carries on — this is a speed-up, never
a reason to fail to start.
