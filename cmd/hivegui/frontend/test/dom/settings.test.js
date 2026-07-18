// @vitest-environment jsdom
//
// Covers the settings modal (src/app/modals/settings.js): the
// add/edit/delete round-trip, the exact payload handed to
// SaveCustomAgents, and the two behaviors that carry real risk —
// existing ids must survive a rename (registry entries persist only
// the agent id, so a changed id breaks revive), and a save rejected by
// Go must surface its error instead of closing the modal.
import { describe, it, expect, vi, beforeAll, beforeEach } from 'vitest';

const listCustomAgents = vi.fn(() => Promise.resolve([]));
const saveCustomAgents = vi.fn(() => Promise.resolve());

vi.mock('../../src/bridge.js', () => ({
  ListCustomAgents: (...a) => listCustomAgents(...a),
  SaveCustomAgents: (...a) => saveCustomAgents(...a),
}));

const MARKUP = `
  <div id="settings" class="hidden">
    <div id="settings-panel">
      <header><h3>Settings</h3><button id="settings-close">×</button></header>
      <section id="settings-agents">
        <div id="settings-agents-list"></div>
        <button id="settings-agent-add">+ Add agent</button>
        <p id="settings-error" class="settings-error hidden"></p>
      </section>
      <div class="actions">
        <button id="settings-cancel">Cancel</button>
        <button id="settings-save" class="primary">Save</button>
      </div>
    </div>
  </div>`;

let openSettings, closeSettings, initSettings, splitCommand;
let refocusActiveTerm, setFocusedTile;

beforeAll(async () => {
  document.body.innerHTML = MARKUP;
  ({ openSettings, closeSettings, initSettings, splitCommand } =
    await import('../../src/app/modals/settings.js'));
  refocusActiveTerm = vi.fn();
  setFocusedTile = vi.fn();
  initSettings({ setFocusedTile, refocusActiveTerm });
});

beforeEach(() => {
  listCustomAgents.mockReset().mockResolvedValue([]);
  saveCustomAgents.mockReset().mockResolvedValue(undefined);
  refocusActiveTerm.mockReset();
  setFocusedTile.mockReset();
  document.getElementById('settings').classList.add('hidden');
});

const el = (id) => document.getElementById(id);
const rows = () => [...document.querySelectorAll('.settings-agent-row')];
const flush = () => new Promise((r) => setTimeout(r, 0));

// Drives an <input> the way a user does: set the value, fire 'input'.
function type(input, value) {
  input.value = value;
  input.dispatchEvent(new window.Event('input', { bubbles: true }));
}

describe('splitCommand', () => {
  it('splits a command line into argv on whitespace', () => {
    expect(splitCommand('claude --model haiku')).toEqual(['claude', '--model', 'haiku']);
  });

  it('collapses extra whitespace and ignores padding', () => {
    expect(splitCommand('  mytool   --fast  ')).toEqual(['mytool', '--fast']);
  });

  it('returns an empty argv for blank input', () => {
    expect(splitCommand('   ')).toEqual([]);
    expect(splitCommand('')).toEqual([]);
    expect(splitCommand(null)).toEqual([]);
  });
});

describe('settings modal', () => {
  it('opens, loads existing agents, and closes', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'claude-lite', name: 'Claude Lite', cmd: ['claude', '--model', 'haiku'], color: '#8b5cf6' },
    ]);

    openSettings();
    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(setFocusedTile).toHaveBeenCalledWith(null);
    await flush();

    expect(rows()).toHaveLength(1);
    expect(rows()[0].querySelector('.settings-agent-name').value).toBe('Claude Lite');
    expect(rows()[0].querySelector('.settings-agent-cmd').value).toBe('claude --model haiku');

    closeSettings();
    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(refocusActiveTerm).toHaveBeenCalled();
  });

  it('adds an agent and saves it with an empty id for Go to assign', async () => {
    openSettings();
    await flush();

    el('settings-agent-add').click();
    expect(rows()).toHaveLength(1);
    type(rows()[0].querySelector('.settings-agent-name'), 'Claude Lite');
    type(rows()[0].querySelector('.settings-agent-cmd'), 'claude --model haiku');

    el('settings-save').click();
    await flush();

    expect(saveCustomAgents).toHaveBeenCalledWith([
      { id: '', name: 'Claude Lite', cmd: ['claude', '--model', 'haiku'], color: '#64748b' },
    ]);
    expect(el('settings').classList.contains('hidden')).toBe(true);
  });

  // The whole reason ids are assigned once in Go: registry entries
  // persist only the agent id, so recomputing it on rename would break
  // revive for every session already created with this agent.
  it('preserves an existing id across a rename', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'claude-lite', name: 'Claude Lite', cmd: ['claude'], color: '#8b5cf6' },
    ]);
    openSettings();
    await flush();

    type(rows()[0].querySelector('.settings-agent-name'), 'Claude Litest');
    el('settings-save').click();
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload[0].id).toBe('claude-lite');
    expect(payload[0].name).toBe('Claude Litest');
  });

  it('deletes the right row when several exist', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'one', name: 'One', cmd: ['one'], color: '#111111' },
      { id: 'two', name: 'Two', cmd: ['two'], color: '#222222' },
      { id: 'three', name: 'Three', cmd: ['three'], color: '#333333' },
    ]);
    openSettings();
    await flush();
    expect(rows()).toHaveLength(3);

    rows()[1].querySelector('.settings-agent-delete').click();
    expect(rows()).toHaveLength(2);

    el('settings-save').click();
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload.map((a) => a.id)).toEqual(['one', 'three']);
  });

  it('drops fully-blank rows so a stray "+ Add agent" does not block the save', async () => {
    openSettings();
    await flush();

    el('settings-agent-add').click();
    type(rows()[0].querySelector('.settings-agent-name'), 'Real');
    type(rows()[0].querySelector('.settings-agent-cmd'), 'realtool');
    el('settings-agent-add').click(); // left entirely blank

    el('settings-save').click();
    await flush();

    const [payload] = saveCustomAgents.mock.calls[0];
    expect(payload).toHaveLength(1);
    expect(payload[0].name).toBe('Real');
  });

  it('surfaces a rejected save and stays open', async () => {
    saveCustomAgents.mockRejectedValue(new Error('"claude" is a built-in agent and cannot be redefined'));
    openSettings();
    await flush();

    el('settings-agent-add').click();
    type(rows()[0].querySelector('.settings-agent-name'), 'Claude');
    type(rows()[0].querySelector('.settings-agent-cmd'), 'claude');
    el('settings-save').click();
    await flush();

    expect(el('settings').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toContain('built-in agent');
  });

  it('reports a failed load without throwing', async () => {
    listCustomAgents.mockRejectedValue(new Error('boom'));
    openSettings();
    await flush();

    expect(el('settings-error').classList.contains('hidden')).toBe(false);
    expect(el('settings-error').textContent).toContain('Could not load');
  });

  it('discards edits on cancel', async () => {
    listCustomAgents.mockResolvedValue([
      { id: 'keep', name: 'Keep', cmd: ['keep'], color: '#111111' },
    ]);
    openSettings();
    await flush();
    type(rows()[0].querySelector('.settings-agent-name'), 'Scribbled');

    el('settings-cancel').click();
    expect(el('settings').classList.contains('hidden')).toBe(true);
    expect(saveCustomAgents).not.toHaveBeenCalled();

    // Reopening re-reads from disk rather than showing the discarded draft.
    openSettings();
    await flush();
    expect(rows()[0].querySelector('.settings-agent-name').value).toBe('Keep');
  });

  it('closes on Escape', async () => {
    openSettings();
    await flush();
    el('settings').dispatchEvent(
      new window.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }),
    );
    expect(el('settings').classList.contains('hidden')).toBe(true);
  });
});
