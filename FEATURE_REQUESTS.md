# Feature Requests

Use this file as the inbox for new feature ideas for Hive. When a request is implemented, move it out of this file and add it to `FEATURES.md`.

## Template

```md
### Feature Name
Status: proposed

Problem / goal:

Proposed behavior:

Notes:
```

## Requests

### Send Keystrokes Without Attaching
Status: proposed

Problem / goal:
Allow sending simple keystrokes to a terminal from the main view and grid view without attaching first. This would make it faster to respond to interactive prompts and menus, such as confirming with `enter`, moving through choices with `up` and `down`, or selecting numbered options.

Proposed behavior:
From the main and grid views, provide a way to send a small set of keystrokes directly to the selected terminal session while staying in the current view. The smallest useful version would support `enter`, arrow keys, and number keys so users can quickly answer prompts with options without switching context or attaching.

Notes:
Keep the interaction discoverable in the UI by showing the available key hints inline. The feature should work consistently in both the main and grid views and should avoid conflicting with existing global navigation shortcuts.
