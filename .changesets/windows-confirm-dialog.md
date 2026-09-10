---
type: fixed
bump: patch
---

Confirmation dialogs now work on Windows. Deleting a project, killing a live
session, restarting Hive and applying an update all go through a native
confirmation, and every one of them silently did nothing on Windows: the dialog
appeared, but answering it always read as a refusal, so the action was abandoned
before it reached the daemon — with no error to show why. Wails ignores our
button labels on Windows and substitutes a native Yes/No, and Hive only
recognised the macOS `OK` as consent. It now accepts the labels each platform
actually reports.
