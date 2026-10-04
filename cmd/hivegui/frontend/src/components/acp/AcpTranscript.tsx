// An ACP session's tile body (spec 496): the transcript the daemon
// folds from the agent's session/update stream, the permission request
// it is blocked on, and a prompt box. Mounted over the tile's hidden
// terminal body (TileChrome.tsx), the way the activity tile is; the
// tile header stays.
//
// Everything the agent wrote is untrusted: messages render through
// Markdown.tsx, which never reaches innerHTML.
import {
  type KeyboardEvent,
  type ReactNode,
  type RefObject,
  memo,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';

import { AnswerPermission, PromptAcp } from '../../bridge.js';
import { reportFailure } from '../../app/dom.js';
import {
  isAllowOption,
  type AcpItem,
  type AcpPermission,
} from '../../lib/acp.js';
import {
  noteSentPrompt,
  returnSentPrompt,
  takeReturnedPrompt,
  useAcpTranscript,
  usePromptPending,
  useReturnedPrompt,
  type AcpLoad,
} from '../../store/acp.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { Icon } from '../Icon.js';
import { Markdown } from '../Markdown.js';

export function AcpTranscript({ sessionId }: { sessionId: string }): ReactNode {
  const info = useAppStore((s) => s.sessions.find((x) => x.id === sessionId));
  const { tx, load } = useAcpTranscript(sessionId, !!info);
  const scroller = useRef<HTMLDivElement>(null);
  // Follow the bottom while the user is there; leave a scroll-up alone.
  const atBottom = useRef(true);
  const prompt = useRef<HTMLTextAreaElement>(null);
  // biome-ignore lint/correctness/useExhaustiveDependencies: re-run on content, not on identity of the ref
  useLayoutEffect(() => {
    const el = scroller.current;
    if (el && atBottom.current) el.scrollTop = el.scrollHeight;
  }, [tx.items, tx.permission]);
  if (!info) return null;
  const working =
    info.state === 'working' || info.state === 'waiting_permission';
  return (
    <div className="acp-transcript">
      <div
        ref={scroller}
        className="acp-transcript__log"
        role="log"
        aria-label="Transcript"
        onScroll={(e) => {
          const el = e.currentTarget;
          atBottom.current =
            el.scrollHeight - el.scrollTop - el.clientHeight < 8;
        }}
      >
        {tx.items.length === 0 ? (
          <Placeholder load={load} />
        ) : (
          tx.items.map((it) => <Item key={it.id} item={it} />)
        )}
        {tx.permission ? (
          <PermissionCard
            // One card per request, so its answered state starts fresh.
            key={tx.permission.request_id}
            sessionId={sessionId}
            perm={tx.permission}
            // The button that was clicked unmounts with the card; without
            // this, keyboard focus would fall to <body>.
            onAnswered={() => prompt.current?.focus()}
          />
        ) : null}
      </div>
      <PromptBox
        sessionId={sessionId}
        busy={working}
        inputRef={prompt}
        // A prompt the user just sent is what they want to see answered.
        onSent={() => {
          atBottom.current = true;
        }}
      />
    </div>
  );
}

function Placeholder({ load }: { load: AcpLoad }): ReactNode {
  const text =
    load === 'loading'
      ? 'Loading…'
      : load === 'failed'
        ? "Couldn't load the transcript — it will retry when Hive reconnects"
        : 'No messages yet. Type a prompt below to start.';
  return <div className="acp-transcript__empty">{text}</div>;
}

// Memoized: the store keeps an unchanged item's object across a delta
// (lib/acp.ts), so a streamed chunk re-renders only the item it grew.
const Item = memo(function Item({ item }: { item: AcpItem }): ReactNode {
  switch (item.kind) {
    case 'user':
      return (
        <div
          className="acp-item acp-item--user"
          data-origin={item.origin || undefined}
        >
          {item.origin && item.origin !== 'user' ? (
            <span className="acp-item__origin">{originLabel(item.origin)}</span>
          ) : null}
          <div className="acp-item__text">{item.text}</div>
        </div>
      );
    case 'agent':
      return (
        <div className="acp-item acp-item--agent">
          <Markdown source={item.text ?? ''} />
        </div>
      );
    case 'thought':
      return (
        <div className="acp-item acp-item--thought">
          <span className="acp-item__origin">Thinking</span>
          <div className="acp-item__text">{item.text}</div>
        </div>
      );
    case 'plan':
      return (
        <ol className="acp-item acp-item--plan" aria-label="Plan">
          {(item.plan ?? []).map((p, i) => (
            <li
              // Plan entries carry no id; the position is the identity.
              // biome-ignore lint/suspicious/noArrayIndexKey: see above
              key={i}
              className="hv-activity__step"
              data-status={planStatus(p.status)}
            >
              <div className="hv-activity__step-row">
                <span className="hv-activity__mark" aria-hidden="true">
                  {p.status === 'completed' ? (
                    <Icon name="check" size={12} />
                  ) : null}
                </span>
                <span className="hv-activity__step-text">{p.content}</span>
              </div>
            </li>
          ))}
        </ol>
      );
    case 'tool':
      return (
        <div
          className="acp-item acp-item--tool hv-activity__call"
          data-running={
            item.status === 'pending' || item.status === 'in_progress'
              ? ''
              : undefined
          }
          data-failed={item.status === 'failed' ? '' : undefined}
        >
          <span className="hv-activity__tool">{item.tool_kind || 'tool'}</span>
          {item.title ? (
            <span className="hv-activity__target">{item.title}</span>
          ) : null}
          <span className="hv-activity__outcome">{item.status ?? ''}</span>
        </div>
      );
    default:
      return null;
  }
});

// ACP plan statuses (pending / in_progress / completed) in the
// activity renderers' vocabulary, so their step styles apply as-is.
function planStatus(s?: string): string {
  if (s === 'completed') return 'done';
  if (s === 'in_progress') return 'active';
  return 'pending';
}

// Who sent a prompt that the user did not type here.
function originLabel(origin: string): string {
  if (origin === 'replayed') return 'Earlier turn';
  if (origin.startsWith('plugin:')) return `From plugin ${origin.slice(7)}`;
  return `From ${origin}`;
}

function PermissionCard({
  sessionId,
  perm,
  onAnswered,
}: {
  sessionId: string;
  perm: AcpPermission;
  onAnswered: () => void;
}): ReactNode {
  // One answer per request: a second click would only earn
  // permission_stale while the daemon's next message is on its way.
  const [answered, setAnswered] = useState(false);
  return (
    <fieldset className="acp-permission" aria-label="Permission request">
      <div className="acp-permission__title">
        <Icon name="state-waiting-permission" size={14} />
        <span>{perm.title || 'The agent asks to use a tool'}</span>
      </div>
      <div className="acp-permission__actions">
        {(perm.options ?? []).map((o) => (
          <Button
            key={o.option_id}
            label={o.name}
            kind={isAllowOption(o) ? 'primary' : 'default'}
            extra={{ 'data-option-kind': o.kind }}
            disabled={answered}
            onClick={() => {
              setAnswered(true);
              AnswerPermission(sessionId, perm.request_id, o.option_id).catch(
                (err: unknown) => {
                  // The answer never left: the agent is still waiting,
                  // so the user must be able to answer again.
                  setAnswered(false);
                  reportFailure('answer permission')(err);
                },
              );
              onAnswered();
            }}
          />
        ))}
      </div>
    </fieldset>
  );
}

function PromptBox({
  sessionId,
  busy,
  inputRef,
  onSent,
}: {
  sessionId: string;
  busy: boolean;
  inputRef: RefObject<HTMLTextAreaElement | null>;
  onSent: () => void;
}): ReactNode {
  const [text, setText] = useState('');
  // The last prompt not yet recorded or refused blocks the next one
  // (store/acp.ts usePromptPending), as a running turn does.
  const pending = usePromptPending(sessionId);
  const blocked = busy || pending;
  // A prompt the daemon refused (store/acp.ts applyAcpError), or one
  // that never reached it, comes back here rather than being lost —
  // unless the user has already started typing another.
  const returned = useReturnedPrompt(sessionId);
  useEffect(() => {
    if (returned === null) return;
    setText((cur) => cur || returned);
    takeReturnedPrompt(sessionId);
  }, [returned, sessionId]);
  const send = () => {
    const t = text.trim();
    if (!t || blocked) return;
    setText('');
    onSent();
    noteSentPrompt(sessionId, t);
    PromptAcp(sessionId, t).catch((err) => {
      returnSentPrompt(sessionId);
      reportFailure('send prompt')(err);
    });
  };
  const onKeyDown = (e: KeyboardEvent<HTMLTextAreaElement>) => {
    // Enter sends and Shift+Enter is a newline — but never mid-IME
    // composition, where Enter commits the composed text. WebKit (the
    // macOS webview) fires that Enter after compositionend, so
    // isComposing is already false; keyCode 229 still marks it.
    if (
      e.key !== 'Enter' ||
      e.shiftKey ||
      e.nativeEvent.isComposing ||
      e.nativeEvent.keyCode === 229
    )
      return;
    e.preventDefault();
    send();
  };
  return (
    <div className="acp-prompt">
      <textarea
        ref={inputRef}
        className="acp-prompt__input"
        data-acp-prompt=""
        aria-label="Prompt"
        rows={2}
        value={text}
        placeholder={busy ? 'The agent is working…' : 'Prompt the agent'}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={onKeyDown}
      />
      <div className="acp-prompt__bar">
        <span className="acp-prompt__hint">
          (enter) send · (shift+enter) newline
        </span>
        <Button
          label="Send"
          kind="primary"
          disabled={blocked || !text.trim()}
          onClick={send}
        />
      </div>
    </div>
  );
}
