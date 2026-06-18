// Fixture for test/e2e/xterm-reflow.spec.js. Boots a real xterm Terminal in
// a real (Chromium) DOM so we can verify what jsdom couldn't: whether
// xterm.js reflows (rewraps) its own scrollback on resize. If it does, the
// daemon's full-ring replay-on-resize is redundant for normal-buffer
// sessions and can eventually be dropped. Exposed on window for the spec.
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';

const api = {
  term: null,
  make(cols, rows) {
    this.term = new Terminal({ cols, rows, scrollback: 5000 });
    this.term.open(document.getElementById('term'));
    return true;
  },
  write(data) {
    return new Promise((resolve) => this.term.write(data, resolve));
  },
  resize(cols, rows) {
    this.term.resize(cols, rows);
  },
  // Collapse physical rows back into logical lines: a wrapped continuation
  // row (isWrapped) is appended to the previous logical line and bumps its
  // physical-row count. Lets the spec assert both "content preserved" and
  // "rewrapped to N rows".
  lines() {
    const buf = this.term.buffer.active;
    const out = [];
    for (let i = 0; i < buf.length; i++) {
      const line = buf.getLine(i);
      if (!line) continue;
      const text = line.translateToString(true);
      if (line.isWrapped && out.length) {
        out[out.length - 1].text += text;
        out[out.length - 1].rows += 1;
      } else {
        out.push({ text, rows: 1 });
      }
    }
    return out.filter((l) => l.text.trim().length);
  },
};

window.__reflow = api;
window.__reflowReady = true;
