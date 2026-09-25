// ---------- plan review modal (#457) ----------
//
// An agent is blocked on this: read its plan, comment on passages, then
// approve it or ask for changes. The flow — fetching, queueing, taking a
// stale review down — lives in app/modals/plan-review.ts; this is only
// the surface.
//
// Comments anchor to text the user selects in the rendered plan. The
// quote is what the agent gets back beside each comment, so it is the
// selected text verbatim rather than a position the agent could not map
// back to its own markdown.
//
// Nothing here takes focus on a button: the review raises itself,
// unprompted, possibly while the user is typing into a terminal, and a
// stray Enter must not approve a plan nobody read. Focus lands on the
// plan text.

import {
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
} from 'react';
import {
  answerPlanReview,
  deferPlanReview,
  type PlanComment,
} from '../../app/modals/plan-review.js';
import { useAppStore } from '../../store/store.js';
import { Button } from '../Button.js';
import { Markdown } from '../Markdown.js';
import { ModalShell } from './ModalShell.js';

// wire.MaxPlanReviewQuoteLen / CommentLen / FeedbackLen / Comments.
const MAX_QUOTE = 4096;
const MAX_COMMENT = 4096;
const MAX_FEEDBACK = 16384;
const MAX_COMMENTS = 200;

const utf8 = new TextEncoder();

/** `s` cut to at most `max` UTF-8 bytes on a code-point boundary. The
 *  daemon measures its caps in bytes and rejects the whole answer when
 *  one is exceeded, so `maxLength` (UTF-16 units) is not enough. */
export function capBytes(s: string, max: number): string {
  if (utf8.encode(s).length <= max) return s;
  let out = '';
  let n = 0;
  for (const ch of s) {
    n += utf8.encode(ch).length;
    if (n > max) break;
    out += ch;
  }
  return out;
}

const SOURCE_NAME: Record<string, string> = { claude: 'Claude', pi: 'Pi' };

export function PlanReview({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) =>
    s.modals.find((m) => m.id === 'plan-review'),
  );
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);
  if (entry?.id !== 'plan-review' || !root) return null;
  return (
    <PlanReviewBody
      key={entry.seq}
      root={root}
      sessionId={entry.sessionId}
      source={entry.source}
      plan={entry.plan}
    />
  );
}

/** The selection inside `el`, trimmed and capped, or ''. */
function selectionIn(el: HTMLElement | null): string {
  const sel = window.getSelection();
  if (!el || !sel || sel.isCollapsed || sel.rangeCount === 0) return '';
  const range = sel.getRangeAt(0);
  if (!el.contains(range.commonAncestorContainer)) return '';
  return capBytes(sel.toString().trim(), MAX_QUOTE);
}

function PlanReviewBody({
  root,
  sessionId,
  source,
  plan,
}: {
  root: HTMLElement;
  sessionId: string;
  source: string;
  plan: string;
}): ReactNode {
  const sessionName = useAppStore(
    (s) => s.sessions.find((x) => x.id === sessionId)?.name,
  );
  const bodyRef = useRef<HTMLElement | null>(null);
  const [selection, setSelection] = useState('');
  const [draft, setDraft] = useState<{ quote: string; text: string } | null>(
    null,
  );
  const [comments, setComments] = useState<PlanComment[]>([]);
  const [feedback, setFeedback] = useState('');
  const [sending, setSending] = useState(false);

  useEffect(() => {
    bodyRef.current?.focus();
  }, []);

  const trackSelection = useCallback(() => {
    setSelection(selectionIn(bodyRef.current));
  }, []);

  const startComment = () => {
    const quote = selection || selectionIn(bodyRef.current);
    if (!quote) return;
    setDraft({ quote, text: '' });
  };

  const addComment = () => {
    if (!draft?.text.trim()) return;
    setComments((cs) => [
      ...cs,
      { quote: draft.quote, text: draft.text.trim() },
    ]);
    setDraft(null);
    setSelection('');
    window.getSelection()?.removeAllRanges();
  };

  const send = (decision: 'approve' | 'deny') => {
    if (sending) return;
    setSending(true);
    void answerPlanReview(decision, comments, feedback.trim()).finally(() =>
      setSending(false),
    );
  };

  const canDeny = comments.length > 0 || feedback.trim() !== '';
  const agent = SOURCE_NAME[source] ?? 'The agent';

  return (
    <ModalShell
      id="plan-review"
      root={root}
      title="Review plan"
      titleSuffix={sessionName ? ` · ${sessionName}` : undefined}
      size="lg"
      onClose={deferPlanReview}
      hints={[{ keys: '[esc]', label: 'decide later' }]}
      actions={
        <>
          <Button
            id="plan-review-deny"
            label="Request changes"
            disabled={!canDeny || sending}
            onClick={() => send('deny')}
          />
          <Button
            id="plan-review-approve"
            label="Approve"
            kind="primary"
            disabled={sending}
            onClick={() => send('approve')}
          />
        </>
      }
    >
      <p className="plan-review-lede">
        {agent} is waiting for you. Select text in the plan to comment on it,
        then approve it or request changes.
      </p>
      {/* A focus stop, like WhatsNew's body: focus lands here rather than
          on a button, and a keyboard user can scroll a long plan. */}
      <section
        id="plan-review-body"
        className="plan-review-plan"
        ref={bodyRef}
        // biome-ignore lint/a11y/noNoninteractiveTabindex: the initial focus target and the scroll handle for a long plan
        tabIndex={0}
        aria-label="The plan"
        onMouseUp={trackSelection}
        onKeyUp={trackSelection}
      >
        <Markdown source={plan} />
      </section>
      <div className="plan-review-tools">
        {draft ? (
          <div className="plan-review-composer" id="plan-review-composer">
            <blockquote className="plan-review-quote">{draft.quote}</blockquote>
            <textarea
              id="plan-review-comment"
              className="hv-input"
              aria-label="Your comment on the selected passage"
              rows={3}
              // biome-ignore lint/a11y/noAutofocus: the user just asked to write this comment
              autoFocus
              value={draft.text}
              onChange={(e) =>
                setDraft({
                  ...draft,
                  text: capBytes(e.target.value, MAX_COMMENT),
                })
              }
              onKeyDown={(e) => {
                if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                  e.preventDefault();
                  addComment();
                }
              }}
            />
            <div className="plan-review-row">
              <Button
                id="plan-review-comment-add"
                label="Add comment"
                kind="primary"
                disabled={!draft.text.trim()}
                onClick={addComment}
              />
              <Button
                id="plan-review-comment-cancel"
                label="Cancel"
                onClick={() => setDraft(null)}
              />
            </div>
          </div>
        ) : (
          <Button
            id="plan-review-comment-start"
            label="Comment on selection"
            disabled={!selection || comments.length >= MAX_COMMENTS}
            onClick={startComment}
          />
        )}
        {comments.length > 0 ? (
          <ol className="plan-review-comments" id="plan-review-comments">
            {comments.map((c, i) => (
              // biome-ignore lint/suspicious/noArrayIndexKey: a comment's position is its identity here
              <li key={i} className="plan-review-comment">
                <blockquote className="plan-review-quote">{c.quote}</blockquote>
                <p>{c.text}</p>
                <Button
                  label="Remove"
                  kind="ghost"
                  onClick={() =>
                    setComments((cs) => cs.filter((_, j) => j !== i))
                  }
                />
              </li>
            ))}
          </ol>
        ) : null}
        <label className="hv-field">
          <span className="hv-field__label">Overall feedback (optional)</span>
          <textarea
            id="plan-review-feedback"
            className="hv-input"
            rows={2}
            value={feedback}
            onChange={(e) =>
              setFeedback(capBytes(e.target.value, MAX_FEEDBACK))
            }
          />
        </label>
      </div>
    </ModalShell>
  );
}
