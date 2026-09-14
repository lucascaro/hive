---
type: fixed
bump: patch
---

Hive no longer gets quietly demoted to Windows' efficiency mode. Windows moves
processes that are not the foreground window to EcoQoS — parked on the
efficiency cores, clocked down — and both halves of Hive fit that description by
design. The session daemon is the one that matters: it runs detached with no
window at all, so it can never be foreground, and it is the half that carries
every byte of agent output through a single-threaded path on its way to your
screen. Nothing had ever told Windows otherwise, so on a machine with
performance and efficiency cores the scheduler was free to decide, and it often
decided wrong — which you felt as a terminal that lagged behind your typing for
no visible reason.

Both the daemon and the window now opt out of execution-speed throttling at
startup, the counterpart to the App Nap opt-out macOS has had all along. On
older Windows, where the API predates the setting, Hive notes it in the log and
carries on: this is a speed-up, never a reason to fail to start.
