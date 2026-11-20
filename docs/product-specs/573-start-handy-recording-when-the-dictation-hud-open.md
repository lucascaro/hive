---
issue: 573
title: Start Handy recording when the dictation HUD opens
type: enhancement
complexity: M
priority: P2
stage: RESEARCH
---

# Start Handy recording when the dictation HUD opens

- **Exec plan:** —

## Problem

Dictating into a session with Handy takes two hotkeys: ⌃⌥Space opens Hive's HUD, then Handy's own key starts recording. The second press is friction on every dictation. It's also easy to forget, which leaves the HUD open and silent. Users who don't use Handy must see no change.

## Desired behavior

- **The setting.** An opt-in setting in GUI Settings, next to the dictation hotkey. It appears only when Handy is installed, and it is off by default.
- **Opening.** With the setting on, pressing ⌃⌥Space opens the HUD and starts Handy recording.
- **Stopping.** The user stops recording either way:
  - with Handy's own key, after which the transcription pastes into the HUD for review before sending;
  - by pressing Enter while still recording, which stops Handy, waits for the transcription, and sends it straight away.
- **Recording ended.** Hive treats a transcription arriving in the HUD as the end of a recording it started.
- **Nothing arrives.** If no transcription arrives after an Enter-stop, the HUD sends nothing, says so, and keeps any typed text.
- **Esc.** Closing the HUD with Esc while recording cancels Handy, so nothing is pasted into whatever window the user returns to.
- **Handy missing.** If the setting is on but Handy is not installed, the HUD behaves exactly as today.
- **Accepted limitation.** Handy can only toggle, so if it was already recording when ⌃⌥Space is pressed, the press stops it instead.

## Success criteria

- With Handy not installed, Settings shows no Handy option, and ⌃⌥Space behaves exactly as in spec 553.
- With Handy installed and the setting off (the default), ⌃⌥Space starts no Handy recording.
- With the setting on, one ⌃⌥Space press opens the HUD with focus, and Handy starts recording with no second key.
- Stopping with Handy's key pastes the transcription into the HUD and sends nothing until the user presses Enter.
- Pressing Enter while a Hive-started recording is active stops Handy and sends the transcription once it arrives, with no further key.
- If no transcription arrives within the wait after an Enter-stop, nothing is sent, the HUD shows why, and any typed text remains.
- Pressing Esc while a Hive-started recording is active cancels Handy, and no text is pasted anywhere afterwards.
- After a transcription has arrived, Enter sends without sending Handy another toggle, so it never starts a new recording.
- The setting persists across restarts of the GUI, menu bar app, and daemon.

## Non-goals

- Bundling, installing, or configuring Handy, or changing its own settings or hotkeys.
- Auto-starting other dictation tools, such as macOS Dictation or Superwhisper.
- Detecting whether Handy is already recording when the HUD opens. That is the accepted limitation above.
- Any platform other than macOS (the HUD is macOS-only).
- A new key for stopping recording inside the HUD.
- Changing how sent text is framed by the target agent. Claude's inbox framing stays as it is.

## Notes

- Handy 0.10.1 remote control: `handy --toggle-transcription`, `--cancel`, `SIGUSR2` (toggle). The docs call the CLI "primarily for Linux users" and don't say whether the flags work on macOS, so this must be verified. There is no state query. Sources: handy.computer/docs/cli, handy.computer/docs/paste-methods.
- Handy pastes via the clipboard and a synthesized ⌘V into the focused window (the HUD's key panel). Paste into the HUD is still unverified from spec 553.
- Builds on spec 553 (dictation HUD, PR #556).
