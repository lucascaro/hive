// Fixture for test/e2e/xterm-reflow.spec.ts. Boots a real xterm Terminal in
// a real (Chromium) DOM so we can verify what jsdom couldn't: whether
// xterm.js reflows (rewraps) its own scrollback on resize. If it does, the
// daemon's full-ring replay-on-resize is redundant for normal-buffer
// sessions and can eventually be dropped. Exposed on window for the spec.
import { Terminal } from '@xterm/xterm';
import '@xterm/xterm/css/xterm.css';

export type ReflowLine = { text: string; rows: number };

export interface ReflowApi {
  make(cols: number, rows: number): boolean;
  write(data: string): Promise<void>;
  resize(cols: number, rows: number): void;
  lines(): ReflowLine[];
}

// The terminal lives in a module-level binding rather than an `api.term`
// field: every method needs it non-null, so one throwing accessor beats a
// guard in each of the four. The spec always calls make() first.
let term: Terminal | null = null;

function activeTerm(): Terminal {
  if (!term) throw new Error('xterm-reflow fixture: make() has not run yet');
  return term;
}

const api: ReflowApi = {
  make(cols, rows) {
    term = new Terminal({ cols, rows, scrollback: 5000 });
    const host = document.getElementById('term');
    if (!host) throw new Error('xterm-reflow fixture: #term is missing');
    term.open(host);
    return true;
  },
  write(data) {
    return new Promise((resolve) => activeTerm().write(data, resolve));
  },
  resize(cols, rows) {
    activeTerm().resize(cols, rows);
  },
  // Collapse physical rows back into logical lines: a wrapped continuation
  // row (isWrapped) is appended to the previous logical line and bumps its
  // physical-row count. Lets the spec assert both "content preserved" and
  // "rewrapped to N rows".
  lines() {
    const buf = activeTerm().buffer.active;
    const out: ReflowLine[] = [];
    for (let i = 0; i < buf.length; i++) {
      const line = buf.getLine(i);
      if (!line) continue;
      const text = line.translateToString(true);
      const prev = out[out.length - 1];
      if (line.isWrapped && prev) {
        prev.text += text;
        prev.rows += 1;
      } else {
        out.push({ text, rows: 1 });
      }
    }
    return out.filter((l) => l.text.trim().length > 0);
  },
};

window.__reflow = api;
window.__reflowReady = true;
