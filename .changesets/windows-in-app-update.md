---
type: added
bump: minor
---

In-app update now works on Windows, on both the release and the latest channel.
Windows previously got "download it manually on this platform" — advice that was
wrong on the latest channel, which tracks a git checkout and has no release
artifact to download at all. The Update → Updating… → Restart button, the
progress lines and the Reload-vs-Restart distinction are the same as on macOS.

Replacing a running `hivegui.exe` needs no helper process and no elevation:
Windows refuses to overwrite or delete a mapped image but allows renaming one,
so the update renames the running binaries aside, installs over them, and sweeps
the displaced copies at the next start. A failure part-way through rolls back,
leaving a launchable Hive.

Two refusals are reported up front rather than after the work: an install
directory Hive cannot write to (move it somewhere you own — `%LOCALAPPDATA%\Programs\Hive`
is the usual spot), and, on the latest channel, running Hive out of the checkout's
own `cmd/hivegui/build/bin`, which the build step has to erase.

The banner also stops telling latest-channel users to "open the releases page
manually", which pointed at a download that does not exist for that channel, and
the button's platform check now comes from the backend — so when an update can't
be applied, the banner says which reason applies instead of blaming the platform.

Note for Windows: the download is checksum-verified but, unlike macOS, not
signature-verified — no Windows release binary is signed, so there is no
publisher to pin.
