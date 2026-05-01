import '@xterm/xterm/css/xterm.css';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';

import { Connect, WriteStdin, Resize } from '../wailsjs/go/main/App';
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

const status = document.getElementById('status');
function setStatus(text, isError = false) {
  status.textContent = text;
  status.classList.toggle('error', isError);
}

const decoder = new TextDecoder('utf-8', { fatal: false });
const decodeB64 = (b64) => {
  const bin = atob(b64);
  const bytes = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
  return decoder.decode(bytes, { stream: true });
};

// During replay we paint scrollback into xterm; once replay-done arrives,
// we treat further DATA as live. This phase distinction matters for
// future features (e.g. hiding cursor flicker during replay) but is
// transparent today.
let phase = 'replay';

EventsOn('pty:data', (b64) => {
  term.write(decodeB64(b64));
});

EventsOn('pty:event', (jsonStr) => {
  try {
    const ev = JSON.parse(jsonStr);
    if (ev.kind === 'scrollback_replay_done') {
      phase = 'live';
      setStatus(`session ${currentSessionId?.slice(0, 8) ?? ''} • live`);
    } else if (ev.kind === 'session_exit') {
      setStatus('session exited', true);
    }
  } catch (e) { /* ignore */ }
});

EventsOn('pty:disconnect', () => {
  setStatus('disconnected', true);
});

EventsOn('pty:error', (jsonStr) => {
  try {
    const e = JSON.parse(jsonStr);
    setStatus(`error: ${e.code}`, true);
    term.write(`\r\n\x1b[31m[hived: ${e.code}: ${e.message}]\x1b[0m\r\n`);
  } catch {
    setStatus('error', true);
  }
});

// Keystrokes -> daemon (UTF-8 -> base64 -> WriteStdin).
term.onData((data) => {
  const bytes = new TextEncoder().encode(data);
  let bin = '';
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]);
  WriteStdin(btoa(bin));
});

let resizeTimer = null;
window.addEventListener('resize', () => {
  if (resizeTimer) clearTimeout(resizeTimer);
  resizeTimer = setTimeout(() => {
    fit.fit();
    Resize(term.cols, term.rows);
  }, 50);
});

term.focus();

let currentSessionId = null;
(async () => {
  try {
    const info = await Connect(term.cols, term.rows);
    currentSessionId = info.sessionId;
    setStatus(`session ${info.sessionId.slice(0, 8)} • replay`);
    // If the daemon's PTY size differs from ours, push our size.
    if (info.cols !== term.cols || info.rows !== term.rows) {
      Resize(term.cols, term.rows);
    }
  } catch (err) {
    setStatus(`connect failed: ${err}`, true);
    term.write(`\r\n\x1b[31m[connect failed: ${err}]\x1b[0m\r\n`);
  }
})();
