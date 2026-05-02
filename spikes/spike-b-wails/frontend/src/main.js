import '@xterm/xterm/css/xterm.css';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

import { StartShell, WriteStdin, Resize } from '../wailsjs/go/main/App';
import { EventsOn } from '../wailsjs/runtime/runtime';

const term = new Terminal({
  fontFamily: 'Menlo, "DejaVu Sans Mono", monospace',
  fontSize: 14,
  cursorBlink: true,
  scrollback: 5000,
  theme: { background: '#000000' },
});
const fit = new FitAddon();
term.loadAddon(fit);
term.open(document.getElementById('term'));
fit.fit();

// PTY bytes -> xterm. Backend base64-encodes for binary safety.
const decoder = new TextDecoder('utf-8', { fatal: false });
EventsOn('pty:data', (b64) => {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  term.write(decoder.decode(bytes, { stream: true }));
});

EventsOn('pty:exit', (msg) => {
  term.write(`\r\n\x1b[33m[pty exit: ${msg}]\x1b[0m\r\n`);
});

// Keystrokes -> PTY. Encode to UTF-8 bytes -> base64.
term.onData((data) => {
  const bytes = new TextEncoder().encode(data);
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  WriteStdin(btoa(bin));
});

// Spawn the shell now that we know the size.
StartShell(term.cols, term.rows).catch((err) => {
  term.write(`\r\n\x1b[31m[StartShell failed: ${err}]\x1b[0m\r\n`);
});

// Window resize -> refit -> push new size to PTY.
let resizeTimer = null;
window.addEventListener('resize', () => {
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    fit.fit();
    Resize(term.cols, term.rows);
  }, 50);
});

term.focus();
