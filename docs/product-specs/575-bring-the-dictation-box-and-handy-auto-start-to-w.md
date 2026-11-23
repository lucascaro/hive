---
issue: 575
title: Bring the dictation box and Handy auto-start to Windows
type: enhancement
complexity: L
priority: P2
pr: 576
stage: GATE
---

# Bring the dictation box and Handy auto-start to Windows

- **Exec plan:** [docs/exec-plans/active/575-bring-the-dictation-box-and-handy-auto-start-to-w.md](../exec-plans/active/575-bring-the-dictation-box-and-handy-auto-start-to-w.md)

## Problem

On Windows, Hive has no way to dictate into a session from another app. The global hotkey and dictation box (spec 553) and Handy auto-start (spec 573) are macOS-only, so a Windows user has to switch to Hive, find the session and type. Handy runs on Windows, but it only pastes into the focused window, so voice doesn't reach an agent unless Hive is already in front.

## Desired behavior

- **Hotkey.** While at least one Hive GUI window is running, pressing Ctrl+Shift+Space anywhere in Windows opens the dictation box over the current app, with keyboard focus.
- **Same box as macOS.**
  - It shows the target session.
  - ↑/↓ switch the target.
  - Shift+Enter inserts a newline.
  - Enter sends.
  - Esc discards.
  - Nothing reaches an agent until the user confirms.
- **Handy auto-start.** The opt-in "Start Handy recording" setting from spec 573 appears in Windows Settings when Handy is installed. It behaves as it does on macOS:
  - opening the box starts recording;
  - Handy's key stops recording for review;
  - Enter while recording stops and sends;
  - Esc cancels Handy.
- **Settings.** The hotkey is rebindable in Settings → Shortcuts. The Handy setting is the same one macOS uses.
- **macOS.** Nothing changes.

## Success criteria

- On Windows, with a Hive GUI window running but not focused, Ctrl+Shift+Space opens the dictation box with keyboard focus. Typed text and Handy-pasted text land in it.
- With several Hive GUI windows open, one press opens exactly one box.
- With no Hive GUI window running, the chord does nothing Hive-related.
- In the box:
  - the target session is visible;
  - ↑/↓ change it;
  - Enter delivers the text to that session only;
  - Esc closes it with nothing written to any session.
- With no live sessions, the box shows an empty state and writes nothing.
- The chord can be rebound in Settings → Shortcuts on Windows, and the new chord takes effect.
- With Handy installed on Windows, Settings shows the "Start Handy recording" option. With Handy not installed, it does not.
- With the option on, one press opens the box and Handy starts recording. Handy's key stops it and pastes for review. Enter while recording stops it and sends. Esc while recording cancels Handy and nothing is pasted anywhere.
- With the option off, the box never starts Handy.
- If Handy's CLI proves unreliable on Windows, the option is hidden on Windows instead and the PR records the evidence. The dictation box criteria above still hold.
- All of the above is verified on a real Windows build, not only by unit tests.
- macOS behaviour and the macOS default hotkey are unchanged.

## Non-goals

- Linux.
- Hotkey or box while no Hive GUI window is running, and any new Windows tray or background agent.
- Installing, bundling or configuring Handy, or detecting whether it is already recording (the same accepted limitation as 573).
- Other dictation tools' auto-start, such as Windows Voice Typing (Win+H).
- Any new entry point that accepts text for a session outside the human-confirmed box.
- Changing the macOS default chord or the cross-platform settings model.

## Notes

- Handy's install path on Windows isn't documented. `%ProgramFiles%\Handy\handy.exe` is a guess taken from a docs example (handy.computer/docs/cli Vulkan section). Verify against the v0.10.1 Windows installer.
- Handy's README says the remote-control flags "work on all platforms (macOS, Windows, Linux). Largely this is a beta feature." SIGUSR2 is not available on Windows. Verify `--toggle-transcription` and `--cancel` on Windows.
- Handy cannot paste into apps running as administrator on Windows.
- Brainstorm decisions:
  - one spec, not split;
  - for the user's own daily Windows use;
  - the hotkey works only while the GUI runs, with no tray agent;
  - the default chord is Ctrl+Shift+Space, which avoids the AltGr clash of Ctrl+Alt;
  - if Handy's CLI fails on Windows, ship the box and hide the Handy option.
- The hivegui app is one process per window, so hotkey registration must not duplicate across windows. That is why the macOS box lives in hivebar.
- Builds on spec 553 (PR #556) and spec 573 (PR #574).
