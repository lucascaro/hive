// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- message a session (spec 338): the non-React half ----------
//
// The sheet renders from components/modals/MessageSession.tsx. What
// stays here is the open/close pair its callers import, the one daemon
// call it makes, and the two entry points events.ts feeds with the
// daemon's answer — SEND_TO_SESSION is fire-and-forget on the Wails
// bridge, so the SENT reply and any refusal arrive later as events.
//
// Same shape as quick-idea.ts, deliberately.

import { flushSync } from 'react-dom';
import { SendToSession } from '../../bridge.js';
import { flashStatus, reportFailure } from '../dom.js';
import {
  anyModalOpen,
  appStore,
  closeModal,
  isModalOpen,
  modalEntry,
  openModal,
  patchModal,
} from '../../store/store.js';
import { pageEl } from '../el.js';
import { releaseFocus } from '../../lib/focus-trap.js';

export interface MessageSessionDeps {
  setFocusedTile: (id: string | null) => void;
  refocusActiveTerm: () => void;
}

let deps: MessageSessionDeps = {
  setFocusedTile: () => {},
  refocusActiveTerm: () => {},
};

/** Bytes the daemon accepts (internal/wire MaxMessageLen). */
export const MAX_MESSAGE_BYTES = 64 << 10;

export function messageTextBytes(text: string): number {
  return new TextEncoder().encode(text).length;
}

export function openMessageSession(sessionId: string): void {
  openModal({ id: 'message-session', sessionId });
  deps.setFocusedTile(null);
}

export function closeMessageSession(): void {
  if (!isModalOpen('message-session')) return;
  releaseFocus(pageEl('message-session'));
  flushSync(() => closeModal('message-session'));
  // Only when this was the last modal: handing focus to the terminal
  // underneath a still-open dialog sends keystrokes to the PTY behind it.
  if (anyModalOpen()) return;
  deps.refocusActiveTerm();
}

/** Send the note. The sheet stays up until the daemon answers, so a
 * refused note (busy, dead, too long) is not lost. */
export function sendMessage(
  sessionId: string,
  text: string,
  queue: boolean,
): void {
  const trimmed = text.trim();
  if (!trimmed) return;
  if (messageTextBytes(trimmed) > MAX_MESSAGE_BYTES) {
    flashStatus(
      `message is too long (max ${MAX_MESSAGE_BYTES / 1024} KiB)`,
      true,
    );
    return;
  }
  patchModal('message-session', { sending: true });
  SendToSession(sessionId, trimmed, queue).catch((err) => {
    patchModal('message-session', { sending: false });
    reportFailure('send message')(err);
  });
}

function nameOf(sessionId: string): string {
  return (
    appStore.getState().sessions.find((s) => s.id === sessionId)?.name ??
    'session'
  );
}

/** events.ts › session:sent. Closes the sheet if it was waiting on this
 * session, and says how the note travelled. */
export function onMessageSent(sessionId: string, delivery: string): void {
  const e = modalEntry('message-session');
  if (e && e.sessionId === sessionId) closeMessageSession();
  const name = nameOf(sessionId);
  flashStatus(
    delivery === 'queued'
      ? `Queued for ${name} — typed when it goes idle`
      : delivery === 'acp'
        ? `Sent to ${name}`
        : delivery === 'socket'
          ? `Sent to ${name} — held for approval if it is in bypass mode`
          : delivery === 'extension'
            ? `Sent to ${name}`
            : `Typed into ${name}`,
  );
}

/** events.ts › control:error. Returns true when the error was a refusal
 * of this sheet's send, so the generic status line is not shown twice. */
export function onMessageError(
  code: string | undefined,
  sessionId: string | undefined,
): boolean {
  const e = modalEntry('message-session');
  if (!e || !sessionId || e.sessionId !== sessionId) return false;
  if (code === 'session_busy') {
    patchModal('message-session', { sending: false, busy: true });
    return true;
  }
  if (
    code === 'session_dead' ||
    code === 'delivery_failed' ||
    code === 'no_such_session' ||
    code === 'bad_payload'
  ) {
    patchModal('message-session', { sending: false });
    flashStatus(refusalWords(code, nameOf(sessionId)), true);
    return true;
  }
  return false;
}

function refusalWords(code: string, name: string): string {
  switch (code) {
    case 'session_dead':
      return `${name} is not running`;
    case 'no_such_session':
      return `${name} is gone`;
    case 'bad_payload':
      return 'nothing to send — the note is empty or too long';
    default:
      return `could not deliver to ${name}`;
  }
}

export function initMessageSession(injected: MessageSessionDeps): void {
  deps = injected;
}
