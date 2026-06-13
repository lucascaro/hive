// ---------- command palette ----------
//
// Moved verbatim from main.js. The command table is BUILT by main.js
// (the actions live there until later stages) and handed to
// initCommandPalette — this module owns only the palette UI.

import { registerModal } from './registry.js';

let deps = {
  focusActiveTerm: () => {},
};
let paletteCommands = [];

export const paletteEl = document.getElementById('command-palette');
const paletteInput = document.getElementById('command-palette-input');
const paletteList = document.getElementById('command-palette-list');
const paletteState = { items: [], selected: 0 };

function renderPalette() {
  const q = paletteInput.value.trim().toLowerCase();
  paletteList.innerHTML = '';
  paletteState.items = paletteCommands.filter((c) => {
    if (!q) return true;
    return c.name.toLowerCase().includes(q) || c.shortcut.toLowerCase().includes(q);
  });
  if (paletteState.selected >= paletteState.items.length) {
    paletteState.selected = 0;
  }
  paletteState.items.forEach((c, i) => {
    const row = document.createElement('div');
    row.className = 'palette-item' + (i === paletteState.selected ? ' selected' : '');
    const name = document.createElement('span');
    name.className = 'palette-name';
    name.textContent = c.name;
    const sc = document.createElement('span');
    sc.className = 'palette-shortcut';
    sc.textContent = c.shortcut;
    row.append(name, sc);
    row.addEventListener('mouseenter', () => {
      paletteState.selected = i;
      for (const el of paletteList.children) el.classList.remove('selected');
      row.classList.add('selected');
    });
    row.addEventListener('click', () => activatePalette(i));
    paletteList.appendChild(row);
  });
}

export function openCommandPalette() {
  paletteInput.value = '';
  paletteState.selected = 0;
  renderPalette();
  paletteEl.classList.remove('hidden');
  paletteInput.focus();
}

export function closeCommandPalette() {
  // Blur first: focusActiveTerm() bails when activeElement is an INPUT,
  // and hiding the palette via CSS doesn't move focus off paletteInput.
  paletteInput.blur();
  paletteEl.classList.add('hidden');
  deps.focusActiveTerm();
}

function activatePalette(i) {
  const c = paletteState.items[i];
  if (!c) return;
  closeCommandPalette();
  // Defer so the palette is fully closed before the action runs
  // (some actions open another modal that owns focus).
  setTimeout(() => c.run(), 0);
}

export function initCommandPalette({ commands, ...injected }) {
  deps = injected;
  paletteCommands = commands;
  registerModal(paletteEl);
  paletteInput.addEventListener('input', renderPalette);
  paletteEl.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') {
      e.preventDefault(); e.stopPropagation();
      closeCommandPalette();
    } else if (e.key === 'ArrowDown' || (e.key === 'Tab' && !e.shiftKey)) {
      e.preventDefault(); e.stopPropagation();
      if (paletteState.items.length === 0) return;
      paletteState.selected = (paletteState.selected + 1) % paletteState.items.length;
      renderPalette();
    } else if (e.key === 'ArrowUp' || (e.key === 'Tab' && e.shiftKey)) {
      e.preventDefault(); e.stopPropagation();
      if (paletteState.items.length === 0) return;
      paletteState.selected = (paletteState.selected - 1 + paletteState.items.length) % paletteState.items.length;
      renderPalette();
    } else if (e.key === 'Enter') {
      e.preventDefault(); e.stopPropagation();
      activatePalette(paletteState.selected);
    }
  });
  document.addEventListener('mousedown', (e) => {
    if (paletteEl.classList.contains('hidden')) return;
    if (!paletteEl.contains(e.target)) closeCommandPalette();
  });
}
