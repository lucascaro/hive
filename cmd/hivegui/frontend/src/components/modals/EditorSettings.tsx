// Settings › Appearance › Editor — which editor ⇧⌘-click on a file
// path opens.
//
// The command lives in Go (editor.json) rather than localStorage for a
// security reason: it is what Hive executes, and OpenFile deliberately
// takes a path and a line number, never a command. See
// cmd/hivegui/editor_prefs.go.

import { isMac } from '../../lib/platform.js';

export type EditorKind =
  | ''
  | 'vscode'
  | 'cursor'
  | 'zed'
  | 'sublime'
  | 'command'
  | 'app';

export interface EditorDraft {
  kind: EditorKind;
  command: string;
  app: string;
}

// Label plus the command each preset runs, shown so the user can see
// which shell command has to be installed — the usual failure is VS
// Code without its `code` launcher.
const PRESETS: { id: EditorKind; label: string }[] = [
  { id: '', label: 'None (use the OS default app)' },
  { id: 'vscode', label: 'VS Code (code)' },
  { id: 'cursor', label: 'Cursor (cursor)' },
  { id: 'zed', label: 'Zed (zed)' },
  { id: 'sublime', label: 'Sublime Text (subl)' },
  { id: 'command', label: 'Custom command…' },
  ...(isMac ? [{ id: 'app' as EditorKind, label: 'Custom application…' }] : []),
];

export function EditorSettings({
  draft,
  onChange,
  disabled,
}: {
  draft: EditorDraft;
  onChange: (next: EditorDraft) => void;
  disabled?: boolean;
}) {
  return (
    <>
      <label className="hv-field">
        <span className="hv-field__label">Editor for ⇧⌘-click</span>
        <select
          id="settings-editor-kind"
          className="hv-input"
          aria-label="Editor for shift-cmd-click"
          value={draft.kind}
          disabled={disabled}
          onChange={(e) =>
            onChange({ ...draft, kind: e.target.value as EditorKind })
          }
        >
          {PRESETS.map((p) => (
            <option key={p.id || 'none'} value={p.id}>
              {p.label}
            </option>
          ))}
        </select>
      </label>
      <p className="settings-hint">
        ⌘-click opens a file with the OS default app; anything that would run as
        a program is revealed in the file manager instead.
      </p>
      {draft.kind === 'command' && (
        <label className="hv-field">
          <span className="hv-field__label">Command</span>
          <input
            id="settings-editor-command"
            type="text"
            className="hv-input hv-input--mono"
            aria-label="Editor command"
            spellCheck={false}
            placeholder="nvim-qt +{line} {file}"
            value={draft.command}
            disabled={disabled}
            onChange={(e) => onChange({ ...draft, command: e.target.value })}
          />
          <span className="settings-hint">
            {'{file}'} is required; {'{line}'} and {'{col}'} are filled in when
            the clicked text has them. Run directly, not through a shell —
            quoting is not needed for paths with spaces.
          </span>
        </label>
      )}
      {draft.kind === 'app' && (
        <label className="hv-field">
          <span className="hv-field__label">Application</span>
          <input
            id="settings-editor-app"
            type="text"
            className="hv-input"
            aria-label="Editor application"
            placeholder="BBEdit"
            value={draft.app}
            disabled={disabled}
            onChange={(e) => onChange({ ...draft, app: e.target.value })}
          />
          <span className="settings-hint">
            Opened with <code>open -a</code>, so the file has no line jump.
          </span>
        </label>
      )}
    </>
  );
}
