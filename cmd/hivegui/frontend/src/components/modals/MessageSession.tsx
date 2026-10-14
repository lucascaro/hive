// ---------- message a session (spec 338) ----------
//
// A one-field sheet: the note, addressed to a named session. Enter sends,
// ⇧Enter is a newline, Escape drops it. If the daemon answers that the
// session is busy the sheet stays up with the text intact and offers to
// queue the note for the session's next idle moment.
//
// Per-open state (the text) is local and resets by remounting on
// `key={entry.seq}`; the daemon's answer reaches the sheet through the
// modal entry (`busy`, `sending`), so a refused send does not remount it.

import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import type { ReactNode } from 'react';
import {
  closeMessageSession,
  MAX_MESSAGE_BYTES,
  messageTextBytes,
  sendMessage,
} from '../../app/modals/message-session.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { ModalShell } from './ModalShell.js';

export function MessageSession({
  root,
}: {
  root: HTMLElement | null;
}): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'message-session'),
  );

  // #message-session sits outside React's tree, so its open/closed class
  // is applied here — see QuickIdea.tsx's identical layout effect.
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);

  if (!entry || entry.id !== 'message-session' || !root) return null;
  return (
    <MessageSheet
      key={entry.seq}
      root={root}
      sessionId={entry.sessionId}
      busy={entry.busy === true}
      sending={entry.sending === true}
    />
  );
}

function MessageSheet({
  root,
  sessionId,
  busy,
  sending,
}: {
  root: HTMLElement;
  sessionId: string;
  busy: boolean;
  sending: boolean;
}): ReactNode {
  const session = useAppStore((s) =>
    s.sessions.find((x) => x.id === sessionId),
  );
  const name = session?.name ?? 'session';
  // An ACP session has no queue: the daemon answers busy whatever we ask,
  // so offering one would be an offer that can never work.
  const canQueue = session?.kind !== 'acp';
  const [text, setText] = useState('');
  const textRef = useRef<HTMLTextAreaElement>(null);
  const bytes = messageTextBytes(text.trim());
  const tooLong = bytes > MAX_MESSAGE_BYTES;
  const nearLimit = bytes > MAX_MESSAGE_BYTES * 0.9;
  const empty = text.trim() === '';

  // Passive effect, for the same reason as QuickIdea.tsx: the root's
  // `hidden` class comes off in the parent's layout effect.
  useEffect(() => {
    textRef.current?.focus();
  }, []);

  function send(queue: boolean) {
    if (empty || tooLong || sending) return;
    sendMessage(sessionId, text, queue);
  }

  // Enter sends, ⇧Enter inserts a newline — spec 217's convention for
  // every multi-line input. When the session is busy Enter is not a send
  // (the sheet is asking a question), so it is left to the buttons.
  function onKeyDown(e: React.KeyboardEvent<HTMLTextAreaElement>) {
    // Enter confirms an IME candidate while composing.
    if (e.key !== 'Enter' || e.shiftKey || e.nativeEvent.isComposing) return;
    e.preventDefault();
    if (!busy) send(false);
  }

  return (
    <ModalShell
      id="message-session"
      root={root}
      size="sm"
      title={`Message ${name}`}
      onClose={closeMessageSession}
      hints={[
        { keys: '[enter]', label: 'send' },
        { keys: '[⇧enter]', label: 'newline' },
        { keys: '[esc]', label: 'cancel' },
      ]}
      actions={
        <>
          <Button
            id="message-session-cancel"
            label="Cancel"
            onClick={closeMessageSession}
          />
          {busy && canQueue ? (
            <Button
              id="message-session-queue"
              label="Queue until idle"
              kind="primary"
              disabled={empty || tooLong || sending}
              onClick={() => send(true)}
            />
          ) : (
            <Button
              id="message-session-send"
              label="Send"
              kind="primary"
              disabled={empty || tooLong || sending || (busy && !canQueue)}
              onClick={() => send(false)}
            />
          )}
        </>
      }
    >
      <>
        <label className="hv-field">
          <span className="hv-field__label">Note</span>
          <textarea
            ref={textRef}
            id="message-session-text"
            className="hv-input hv-idea-text"
            rows={3}
            autoComplete="off"
            aria-label={`Message to ${name}`}
            placeholder="What should it know?"
            value={text}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={onKeyDown}
          />
          {nearLimit ? (
            <span
              className="hv-idea-count"
              id="message-session-count"
              data-over={tooLong ? '' : undefined}
            >
              {bytes} / {MAX_MESSAGE_BYTES} bytes
            </span>
          ) : null}
        </label>
        {busy ? (
          <p id="message-session-busy" className="hv-field__hint" role="status">
            {canQueue
              ? `${name} is working. Queue the note to type it when it next goes idle?`
              : `${name} is mid-turn. Try again when it finishes.`}
          </p>
        ) : (
          <p className="hv-field__hint">
            Typed into {name} when it is quiet. A session waiting at a prompt
            you have not answered may take it as the answer.
          </p>
        )}
      </>
    </ModalShell>
  );
}
