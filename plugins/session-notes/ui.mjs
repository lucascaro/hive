// Session notes — the example UI plugin. Written only against
// docs/plugins.md: it uses every app surface a plugin can contribute
// (session view, command palette, sidebar badge, settings section) and
// keeps its data in its plugin settings, so it needs no process of its
// own and no build step.

export default function activate(hive) {
  const { React, components } = hive;
  const h = React.createElement;
  const { useState } = React;

  // Settings: { notes: { [sessionId]: text }, showBadge, template }.
  const notesOf = (cfg) => cfg.notes ?? {};
  const noteFor = (id) => notesOf(hive.settings.get())[id] ?? '';
  const firstLine = (text) => text.split('\n')[0].slice(0, 80);

  function saveNote(id, text) {
    const cfg = hive.settings.get();
    const notes = { ...notesOf(cfg) };
    if (text.trim()) notes[id] = text;
    else delete notes[id];
    hive.settings.set({ ...cfg, notes });
  }

  function editNote(sessionId) {
    if (sessionId) hive.openSessionView(sessionId);
  }

  // The session view: edit this session's note.
  function NoteEditor({ session, close }) {
    const cfg = hive.settings.use();
    const [text, setText] = useState(notesOf(cfg)[session.id] ?? cfg.template ?? '');
    return h(
      'div',
      { className: 'session-notes-editor' },
      h('textarea', {
        id: 'session-notes-text',
        className: 'session-notes-text',
        'aria-label': `Note for ${session.name ?? 'this session'}`,
        rows: 6,
        value: text,
        autoFocus: true,
        onChange: (e) => setText(e.target.value),
      }),
      h(
        'div',
        { className: 'session-notes-actions' },
        h(components.Button, {
          id: 'session-notes-clear',
          label: 'Clear',
          onClick: () => {
            saveNote(session.id, '');
            close();
          },
        }),
        h(components.Button, {
          id: 'session-notes-save',
          label: 'Save',
          kind: 'primary',
          onClick: () => {
            saveNote(session.id, text);
            close();
          },
        }),
      ),
    );
  }

  // The side panel: every note, newest session first; click to go there.
  function NotesPanel() {
    const cfg = hive.settings.use();
    const sessions = hive.useSessions();
    const notes = notesOf(cfg);
    const withNotes = sessions.filter((s) => notes[s.id]);
    if (withNotes.length === 0) {
      return h('p', { className: 'session-notes-empty' }, 'No notes yet.');
    }
    return h(
      'ul',
      { className: 'session-notes-list' },
      withNotes.map((s) =>
        h(
          'li',
          { key: s.id },
          h(
            'button',
            {
              type: 'button',
              className: 'session-notes-item',
              onClick: () => hive.actions.switchTo(s.id),
            },
            h('strong', null, s.name ?? s.id),
            h('span', null, firstLine(notes[s.id])),
          ),
        ),
      ),
    );
  }

  // The settings section. The template field saves on Enter, which is
  // its own action — Settings leaves Enter here to the plugin.
  function NotesSettings() {
    const cfg = hive.settings.use();
    const [template, setTemplate] = useState(cfg.template ?? '');
    return h(
      'div',
      { className: 'session-notes-settings' },
      h(
        'label',
        { className: 'settings-check' },
        h('input', {
          id: 'session-notes-show-badge',
          type: 'checkbox',
          checked: cfg.showBadge !== false,
          onChange: (e) => hive.settings.set({ ...cfg, showBadge: e.target.checked }),
        }),
        h('span', null, 'Show a badge on sessions with a note'),
      ),
      h(
        'label',
        { className: 'hv-field' },
        h('span', { className: 'hv-field__label' }, 'New note template [enter] saves'),
        h('input', {
          id: 'session-notes-template',
          type: 'text',
          className: 'hv-input',
          value: template,
          onChange: (e) => setTemplate(e.target.value),
          onKeyDown: (e) => {
            if (e.key === 'Enter') {
              e.preventDefault();
              hive.settings.set({ ...cfg, template });
            }
          },
        }),
      ),
    );
  }

  return {
    sessionView: {
      modal: {
        title: 'Session note',
        hints: [{ keys: '[esc]', label: 'close without saving' }],
        component: NoteEditor,
      },
      panel: { title: 'Session notes', component: NotesPanel },
      banner: (session) => {
        const note = noteFor(session.id);
        if (!note) return null;
        return {
          text: `Note: ${firstLine(note)}`,
          action: { label: 'Edit', run: () => editNote(session.id) },
        };
      },
    },
    commands: [
      {
        id: 'toggle-panel',
        title: 'Toggle session notes panel',
        keys: { key: 'O', shift: true },
        run: () => hive.togglePanel(),
      },
      {
        id: 'edit-note',
        title: 'Edit note for this session',
        run: () => editNote(hive.getActiveSessionId()),
      },
    ],
    badge: (session) => {
      const cfg = hive.settings.get();
      if (cfg.showBadge === false || !notesOf(cfg)[session.id]) return null;
      return { text: 'Note', title: firstLine(notesOf(cfg)[session.id]), tone: 'info' };
    },
    settings: NotesSettings,
  };
}
