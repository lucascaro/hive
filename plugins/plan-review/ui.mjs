// Plan review — the bundled plugin that shows an agent's plan in Hive
// before it runs (#457, moved out of the app by spec 471). Written only
// against docs/plugins.md, like any third-party plugin.
//
// An agent (Claude at ExitPlanMode, Pi through hive_submit_plan) is
// blocked until the user approves or denies its plan. The daemon parks
// the review as session data: a session's pending_plan_review carries
// only its id, and the plan text is fetched once with getPlanReview
// (answered on the "planreview:plan" event), because session info rides
// every broadcast and a plan can run to 128 KiB. The daemon parks a
// review only while some window runs this plugin's UI; with none, the
// agent falls back to its own terminal prompt.
//
// Reviews are raised one at a time: a second agent planning at the same
// time queues behind the first rather than taking it off the screen
// unseen. Escape DEFERS, it never answers: the agent keeps waiting, its
// terminal still shows its own approval prompt, and the banner for the
// active session is the way back in. A review that ends elsewhere —
// answered in another window or in the terminal, the session exiting —
// takes the view down with it.

// wire.MaxPlanReviewQuoteLen / CommentLen / FeedbackLen / Comments.
const MAX_QUOTE = 4096;
const MAX_COMMENT = 4096;
const MAX_FEEDBACK = 16384;
const MAX_COMMENTS = 200;

const SOURCE_NAME = { claude: 'Claude', pi: 'Pi' };

const utf8 = new TextEncoder();

/** `s` cut to at most `max` UTF-8 bytes on a code-point boundary. The
 *  daemon measures its caps in bytes and rejects the whole answer when
 *  one is exceeded, so a character count is not enough. */
export function capBytes(s, max) {
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

/** The session's pending review id, reading either spelling, or ''. */
function reviewIdOf(s) {
  const p = s?.pending_plan_review ?? s?.pendingPlanReview;
  return p?.review_id ?? p?.reviewId ?? '';
}

function parse(v) {
  if (typeof v !== 'string') return v ?? {};
  try {
    return JSON.parse(v);
  } catch {
    return {};
  }
}

export default function activate(hive) {
  const { React, components } = hive;
  const h = React.createElement;
  const { useCallback, useEffect, useRef, useState } = React;

  // ---------- the queue ----------

  // session id → the review it is waiting on, in arrival order.
  const pending = new Map();
  // Reviews the user deferred with Escape. Not re-raised on their own;
  // raise() is the deliberate way back.
  const deferred = new Set();
  // The review whose text is being fetched, and the one on screen.
  let fetching = null;
  let open = null;

  function sync(sessions) {
    const live = new Set();
    for (const s of sessions) {
      live.add(s.id);
      const reviewId = reviewIdOf(s);
      const prev = pending.get(s.id);
      if (prev && prev !== reviewId) deferred.delete(prev);
      if (reviewId) pending.set(s.id, reviewId);
      else pending.delete(s.id);
    }
    for (const [id, reviewId] of pending) {
      if (!live.has(id)) {
        deferred.delete(reviewId);
        pending.delete(id);
      }
    }
    // The review on screen ended or was replaced by a newer plan: take
    // it down. Nothing is answered; the newer plan is raised next.
    if (open && pending.get(open.sessionId) !== open.reviewId) {
      open.ended = true;
      open = null;
      hive.closeSessionView();
    }
    raiseNext();
  }

  function raiseNext() {
    if (fetching || open) return;
    for (const [sessionId, reviewId] of pending) {
      if (deferred.has(reviewId)) continue;
      fetchReview(sessionId, reviewId);
      return;
    }
  }

  function fetchReview(sessionId, reviewId) {
    fetching = { sessionId, reviewId };
    Promise.resolve(hive.actions.getPlanReview(sessionId, reviewId)).catch((err) => {
      console.warn('plan-review: fetching the plan failed', err);
      fetching = null;
    });
  }

  function onPlanText(msg) {
    const sessionId = msg.session_id ?? '';
    const reviewId = msg.review_id ?? '';
    if (!fetching || fetching.reviewId !== reviewId) return;
    fetching = null;
    // Still the review this session waits on? It may have been answered
    // elsewhere, or replaced, while the text was on its way.
    if (pending.get(sessionId) !== reviewId || open) {
      raiseNext();
      return;
    }
    const view = { sessionId, reviewId, ended: false };
    open = view;
    hive
      .openSessionView(sessionId, { reviewId, source: msg.source ?? '', plan: msg.plan ?? '' })
      .then((result) => {
        if (open === view) open = null;
        // Escape or the close button: decide later.
        if (result === 'dismissed' && !view.ended) deferred.add(reviewId);
        raiseNext();
      });
  }

  // GET_PLAN_REVIEW found nothing pending: that review is over. Forget
  // it before moving on, or the queue would fetch the same stale review
  // forever. A newer review arrives on its own session event.
  function onStale(sessionId) {
    const was = fetching;
    fetching = null;
    const id = sessionId || was?.sessionId || '';
    if (was && pending.get(id) === was.reviewId) pending.delete(id);
    raiseNext();
  }

  /** Open the review a session is waiting on, including a deferred one. */
  function raise(sessionId) {
    const reviewId = pending.get(sessionId);
    if (!reviewId) return;
    deferred.delete(reviewId);
    if (open?.reviewId === reviewId) return;
    // Something else is on screen or on its way: no longer deferred,
    // this one is raised when its turn comes.
    if (open || fetching) return;
    fetchReview(sessionId, reviewId);
  }

  hive.on('planreview:plan', (msg) => onPlanText(parse(msg)));
  hive.on('control:error', (e) => {
    const err = parse(e);
    if (err.code === 'plan_review_stale') onStale(err.session_id);
  });
  hive.subscribeSessions(sync);
  sync(hive.getSessions());

  // ---------- the review ----------

  /** The selection inside `el`, trimmed and capped, or ''. */
  function selectionIn(el) {
    const sel = window.getSelection();
    if (!el || !sel || sel.isCollapsed || sel.rangeCount === 0) return '';
    if (!el.contains(sel.getRangeAt(0).commonAncestorContainer)) return '';
    return capBytes(sel.toString().trim(), MAX_QUOTE);
  }

  // Nothing here takes focus on a button: the review raises itself,
  // unprompted, possibly while the user is typing into a terminal, and a
  // stray Enter must not approve a plan nobody read. Focus lands on the
  // plan text.
  function Review({ session, props, close }) {
    const { reviewId, source, plan } = props ?? {};
    const bodyRef = useRef(null);
    const [selection, setSelection] = useState('');
    const [draft, setDraft] = useState(null);
    const [comments, setComments] = useState([]);
    const [feedback, setFeedback] = useState('');
    const [sending, setSending] = useState(false);
    const [error, setError] = useState('');

    useEffect(() => {
      bodyRef.current?.focus();
    }, []);

    const trackSelection = useCallback(() => setSelection(selectionIn(bodyRef.current)), []);

    const startComment = () => {
      const quote = selection || selectionIn(bodyRef.current);
      if (quote) setDraft({ quote, text: '' });
    };

    const addComment = () => {
      if (!draft?.text.trim()) return;
      setComments((cs) => [...cs, { quote: draft.quote, text: draft.text.trim() }]);
      setDraft(null);
      setSelection('');
      window.getSelection()?.removeAllRanges();
    };

    const send = (decision) => {
      if (sending) return;
      setSending(true);
      setError('');
      Promise.resolve(
        hive.actions.resolvePlanReview({
          session_id: session.id,
          review_id: reviewId,
          decision,
          comments,
          feedback: feedback.trim(),
        }),
      ).then(
        () => close(),
        (err) => {
          setSending(false);
          setError(`Could not send your answer: ${err?.message ?? err}`);
        },
      );
    };

    const canDeny = comments.length > 0 || feedback.trim() !== '';
    const agent = SOURCE_NAME[source] ?? 'The agent';

    return h(
      'div',
      { className: 'plan-review' },
      h(
        'p',
        { className: 'plan-review-lede' },
        `${agent} is waiting for you. Select text in the plan to comment on it, then approve it or request changes.`,
      ),
      // A focus stop: focus lands here rather than on a button, and a
      // keyboard user can scroll a long plan.
      h(
        'section',
        {
          id: 'plan-review-body',
          className: 'plan-review-plan',
          ref: bodyRef,
          tabIndex: 0,
          'aria-label': 'The plan',
          onMouseUp: trackSelection,
          onKeyUp: trackSelection,
        },
        h(components.Markdown, { source: plan ?? '' }),
      ),
      h(
        'div',
        { className: 'plan-review-tools' },
        draft
          ? h(
              'div',
              { className: 'plan-review-composer', id: 'plan-review-composer' },
              h('blockquote', { className: 'plan-review-quote' }, draft.quote),
              h('textarea', {
                id: 'plan-review-comment',
                className: 'hv-input',
                'aria-label': 'Your comment on the selected passage',
                rows: 3,
                autoFocus: true,
                value: draft.text,
                onChange: (e) => setDraft({ ...draft, text: capBytes(e.target.value, MAX_COMMENT) }),
                onKeyDown: (e) => {
                  if (e.key === 'Enter' && (e.metaKey || e.ctrlKey)) {
                    e.preventDefault();
                    addComment();
                  }
                },
              }),
              h(
                'div',
                { className: 'plan-review-row' },
                h(components.Button, {
                  id: 'plan-review-comment-add',
                  label: 'Add comment',
                  kind: 'primary',
                  disabled: !draft.text.trim(),
                  onClick: addComment,
                }),
                h(components.Button, {
                  id: 'plan-review-comment-cancel',
                  label: 'Cancel',
                  onClick: () => setDraft(null),
                }),
              ),
            )
          : h(components.Button, {
              id: 'plan-review-comment-start',
              label: 'Comment on selection',
              disabled: !selection || comments.length >= MAX_COMMENTS,
              onClick: startComment,
            }),
        comments.length > 0
          ? h(
              'ol',
              { className: 'plan-review-comments', id: 'plan-review-comments' },
              comments.map((c, i) =>
                h(
                  'li',
                  { key: i, className: 'plan-review-comment' },
                  h('blockquote', { className: 'plan-review-quote' }, c.quote),
                  h('p', null, c.text),
                  h(components.Button, {
                    label: 'Remove',
                    kind: 'ghost',
                    onClick: () => setComments((cs) => cs.filter((_, j) => j !== i)),
                  }),
                ),
              ),
            )
          : null,
        h(
          'label',
          { className: 'hv-field' },
          h('span', { className: 'hv-field__label' }, 'Overall feedback (optional)'),
          h('textarea', {
            id: 'plan-review-feedback',
            className: 'hv-input',
            rows: 2,
            value: feedback,
            onChange: (e) => setFeedback(capBytes(e.target.value, MAX_FEEDBACK)),
          }),
        ),
      ),
      error ? h('p', { className: 'plan-review-error', role: 'alert' }, error) : null,
      h(
        'div',
        { className: 'plan-review-actions' },
        h(components.Button, {
          id: 'plan-review-deny',
          label: 'Request changes',
          disabled: !canDeny || sending,
          onClick: () => send('deny'),
        }),
        h(components.Button, {
          id: 'plan-review-approve',
          label: 'Approve',
          kind: 'primary',
          disabled: sending,
          onClick: () => send('approve'),
        }),
      ),
    );
  }

  // ---------- settings ----------

  // Who reviews a Claude plan when another reviewer is installed.
  // Applies to newly started sessions: Hive can switch a Claude Code
  // plugin reviewer off only when it starts the session.
  function ReviewSettings() {
    const cfg = hive.settings.use();
    const reviewer = cfg.reviewer === 'hive' ? 'hive' : 'external';
    const [external, setExternal] = useState([]);
    useEffect(() => {
      let live = true;
      // Detection is advice for the warning below, never a reason to
      // fail the section.
      Promise.resolve(hive.actions.externalPlanReviewers())
        .then((rs) => {
          if (live) setExternal(rs ?? []);
        })
        .catch(() => {});
      return () => {
        live = false;
      };
    }, []);
    return h(
      'div',
      { className: 'plan-review-settings' },
      h(
        'label',
        { className: 'hv-field' },
        h('span', { className: 'hv-field__label' }, 'When another plan reviewer is installed'),
        h(
          'select',
          {
            id: 'plan-review-reviewer',
            className: 'hv-input',
            value: reviewer,
            'aria-describedby': 'plan-review-reviewer-hint',
            onChange: (e) => hive.settings.set({ ...cfg, reviewer: e.target.value }),
          },
          h('option', { value: 'external' }, "Let that tool review Claude's plans"),
          h('option', { value: 'hive' }, "Review Claude's plans in Hive instead"),
        ),
      ),
      h(
        'p',
        { id: 'plan-review-reviewer-hint', className: 'settings-hint' },
        'Choosing Hive disables a reviewer plugin, such as plannotator, for the whole of each Claude session Hive starts — its commands too — and applies to newly started sessions only.',
      ),
      reviewer === 'hive'
        ? external
            .filter((r) => r.kind === 'settings')
            .map((r) =>
              h(
                'p',
                {
                  key: r.id,
                  className: 'settings-hint settings-warning',
                  id: 'plan-review-reviewer-warning',
                },
                `${r.id} also reviews Claude's plans, and Hive cannot switch a hook in a settings file off: both will prompt. Remove it there, or let that tool review.`,
              ),
            )
        : null,
    );
  }

  return {
    sessionView: {
      modal: {
        title: (session) => (session?.name ? `Review plan · ${session.name}` : 'Review plan'),
        hints: [{ keys: '[esc]', label: 'decide later' }],
        component: Review,
      },
      banner: (session) =>
        reviewIdOf(session)
          ? {
              text: `${session.name ?? 'This session'} is waiting for you to review its plan`,
              action: { label: 'Review plan…', run: () => raise(session.id) },
            }
          : null,
    },
    commands: [
      {
        id: 'review-pending',
        title: 'Review pending plan',
        run: () => {
          const active = hive.getActiveSessionId();
          raise(active && pending.has(active) ? active : [...pending.keys()][0]);
        },
      },
    ],
    badge: (session) =>
      reviewIdOf(session) ? { text: 'Review', title: 'Waiting for you to review its plan', tone: 'warn' } : null,
    settings: ReviewSettings,
  };
}
