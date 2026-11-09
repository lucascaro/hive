// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// One node's detail pane: facts, the wait it is blocked on (a human
// question or an ACP permission), its output and its activity.
//
// Everything here is engine or agent text and untrusted: prompts, errors,
// file names and output render as plain React text through
// lib/workflow-text.ts; only agent.message goes through Markdown.tsx, which
// uses marked's lexer and never an HTML renderer.

import { type FormEvent, type ReactNode, useEffect, useState } from 'react';
import {
  WorkflowError,
  fetchHistory,
  getNode,
  openNodeSession,
  request,
  selectNode,
} from '../../app/workflows.js';
import { allNodes, localId, nodeIdOfKey } from '../../lib/workflow-graph.js';
import { type OverlayState, instancesOf } from '../../lib/workflow-overlay.js';
import { describe, initialRaw, toValue } from '../../lib/schema-form.js';
import { plainText, showControls, stripCsi } from '../../lib/workflow-text.js';
import type {
  IR,
  NodeGet,
  RunEvent,
  Waiting,
} from '../../lib/workflow-types.js';
import {
  answerDraftId,
  dropDraft,
  setDraft,
  useWorkflows,
} from '../../store/workflows.js';
import { Markdown } from '../Markdown.js';
import { Button } from '../Button.js';
import { PermissionCard } from '../acp/PermissionCard.js';
import { SchemaForm } from './SchemaForm.js';
import { StateMark } from './StateMark.js';

export function NodeDetail({
  runId,
  nodeKey,
  ir,
  overlay,
}: {
  runId: string;
  nodeKey: string;
  ir: IR;
  overlay: OverlayState;
}): ReactNode {
  const node = allNodes(ir).find((n) => n.id === nodeIdOfKey(nodeKey));
  const inst = overlay.instances[nodeKey];
  const state = inst?.state ?? 'pending';
  const attempt = inst?.attempt ?? 0;

  const [facts, setFacts] = useState<NodeGet | null>(null);
  const [factsError, setFactsError] = useState<string | null>(null);
  const [output, setOutput] = useState('');
  const [more, setMore] = useState(false);
  const [next, setNext] = useState(0);
  const [loadingMore, setLoadingMore] = useState(false);
  const stamp = inst ? `${inst.state}:${inst.attempt}` : 'none';

  // node.get on select, and again when the node's state or attempt moves
  // (a wait ending, a retry): the slim run.nodes rows carry none of this.
  // The first slice replaces the output; Load more appends later ones.
  useEffect(() => {
    if (!inst) {
      setFacts(null);
      setOutput('');
      setMore(false);
      return;
    }
    let live = true;
    setFactsError(null);
    getNode(runId, nodeKey, 0)
      .then((g) => {
        if (!live) return;
        setFacts(g);
        setOutput(g.output ?? '');
        setMore(!!g.more);
        setNext(g.next_offset ?? (g.output ?? '').length);
      })
      .catch((e: unknown) => {
        if (live) setFactsError(e instanceof Error ? e.message : String(e));
      });
    return () => {
      live = false;
    };
  }, [runId, nodeKey, stamp, !!inst]);

  // The activity history is paged lazily, once per opened node.
  useEffect(() => {
    void fetchHistory(runId, nodeKey);
  }, [runId, nodeKey]);

  const loadMore = () => {
    setLoadingMore(true);
    getNode(runId, nodeKey, next)
      .then((g) => {
        // Appended as text; a partial page is never parsed.
        setOutput((o) => o + (g.output ?? ''));
        setMore(!!g.more);
        setNext(g.next_offset ?? next + (g.output ?? '').length);
      })
      .catch((e: unknown) =>
        setFactsError(e instanceof Error ? e.message : String(e)),
      )
      .finally(() => setLoadingMore(false));
  };

  const waiting: Waiting | undefined =
    state === 'waiting' ? (inst?.waiting ?? facts?.waiting) : undefined;
  const kind =
    node?.kind === 'agent' ? `agent ${node.agent}` : (node?.kind ?? 'node');
  const siblings = instancesOf(overlay, nodeIdOfKey(nodeKey));
  const history = useWorkflows((s) => s.history);
  const events =
    history && history.runId === runId && history.key === nodeKey
      ? history.events
      : [];

  const sessionId =
    facts?.session_id ??
    (waiting?.reason === 'permission' ? waiting.session_id : undefined);
  const error = inst?.error ?? facts?.error;
  const conflict = inst?.conflict ?? facts?.conflict;

  return (
    <section
      className="hv-wf-detail"
      aria-label={`${plainText(localId(nodeKey))} details`}
    >
      <header className="hv-wf-detail__head">
        <h4 className="hv-wf-detail__title">{plainText(localId(nodeKey))}</h4>
        <span className="hv-wf-detail__kind">{plainText(kind)}</span>
        <StateMark
          state={state}
          permission={waiting?.reason === 'permission'}
        />
      </header>
      <dl className="hv-wf-facts">
        <dt>Key</dt>
        <dd>{showControls(nodeKey)}</dd>
        <dt>Attempt</dt>
        <dd>{attempt}</dd>
        {facts?.branch ? (
          <>
            <dt>Branch</dt>
            <dd>{showControls(facts.branch)}</dd>
          </>
        ) : null}
        {facts?.worktree ? (
          <>
            <dt>Worktree</dt>
            <dd>{showControls(facts.worktree)}</dd>
          </>
        ) : null}
        {inst?.cap ? (
          <>
            <dt>Stopped by</dt>
            <dd>{plainText(inst.cap)}</dd>
          </>
        ) : null}
      </dl>
      {siblings.length > 1 ? (
        <div
          className="hv-wf-instances"
          role="group"
          aria-label="Rounds and copies"
        >
          {siblings
            .map(([k]) => k)
            .sort((a, b) => a.localeCompare(b, undefined, { numeric: true }))
            .map((k) => (
              <button
                key={k}
                type="button"
                className="hv-wf-instance"
                aria-pressed={k === nodeKey}
                onClick={() => selectNode(k)}
              >
                {showControls(k)}
              </button>
            ))}
        </div>
      ) : null}
      {factsError ? (
        <p className="hv-wf-error" role="alert">
          {plainText(factsError)}
        </p>
      ) : null}
      {error ? (
        <p className="hv-wf-error" role="alert">
          {plainText(error)}
        </p>
      ) : null}
      {conflict ? (
        <div className="hv-wf-conflict" role="alert">
          <p>
            This node's changes conflict with the branch{' '}
            <code>{showControls(conflict.branch)}</code>. Resolve the conflict
            in <code>{showControls(conflict.worktree)}</code>, then retry the
            run.
          </p>
          <ul>
            {conflict.files.map((f) => (
              <li key={f}>{showControls(f)}</li>
            ))}
          </ul>
        </div>
      ) : null}
      {waiting?.reason === 'human' ? (
        <HumanAnswer runId={runId} nodeKey={nodeKey} waiting={waiting} />
      ) : null}
      {waiting?.reason === 'permission' ? (
        <PermissionCard
          sessionId={waiting.session_id}
          perm={{
            request_id: waiting.request_id,
            title: plainText(waiting.title),
            options: waiting.options.map((o) => ({
              ...o,
              name: plainText(o.name),
            })),
          }}
          onAnswered={() => {}}
        />
      ) : null}
      {sessionId ? (
        <Button
          label="Open session"
          icon="external"
          onClick={() => openNodeSession(sessionId)}
        />
      ) : null}
      {output || more ? (
        <div className="hv-wf-output-wrap">
          <h5 className="hv-wf-section">Output</h5>
          <pre className="hv-wf-output">{plainText(output)}</pre>
          {more ? (
            <Button
              label="Load more"
              disabled={loadingMore}
              onClick={loadMore}
            />
          ) : null}
        </div>
      ) : null}
      <Activity events={events} loading={history?.status === 'loading'} />
    </section>
  );
}

function Activity({
  events,
  loading,
}: {
  events: RunEvent[];
  loading: boolean;
}): ReactNode {
  if (events.length === 0 && !loading) return null;
  return (
    <div className="hv-wf-activity">
      <h5 className="hv-wf-section">Activity</h5>
      {loading && events.length === 0 ? (
        <p className="hv-wf-muted">Loading activity...</p>
      ) : null}
      {events.map((ev) => {
        const d = ev.data as Record<string, unknown>;
        if (ev.type === 'agent.message')
          return (
            <div key={ev.seq} className="hv-wf-activity__message">
              <Markdown source={plainText(String(d.text ?? ''))} />
            </div>
          );
        if (ev.type === 'check.output')
          return (
            <pre key={ev.seq} className="hv-wf-output">
              {stripCsi(String(d.text ?? ''))}
            </pre>
          );
        if (ev.type === 'agent.tool')
          return (
            <p key={ev.seq} className="hv-wf-activity__tool">
              {plainText(String(d.title ?? 'tool'))} (
              {plainText(String(d.status ?? ''))})
            </p>
          );
        return null;
      })}
    </div>
  );
}

function HumanAnswer({
  runId,
  nodeKey,
  waiting,
}: {
  runId: string;
  nodeKey: string;
  waiting: Extract<Waiting, { reason: 'human' }>;
}): ReactNode {
  const id = answerDraftId(runId, nodeKey);
  const draft = useWorkflows((s) => s.drafts[id]);
  const [problem, setProblem] = useState<string | null>(null);
  const [sending, setSending] = useState(false);
  const schema = waiting.output_schema;

  const submit = (e: FormEvent) => {
    e.preventDefault();
    const { value, errors } = toValue(
      schema,
      draft ?? initialRaw(describe(schema)),
    );
    if (errors.length) {
      setProblem(errors.join('\n'));
      return;
    }
    setProblem(null);
    setSending(true);
    request('human.answer', { run_id: runId, key: nodeKey, answer: value })
      .then(() => dropDraft(id))
      .catch((err: unknown) => {
        const code = err instanceof WorkflowError ? err.code : '';
        const msg = err instanceof Error ? err.message : String(err);
        setProblem(
          code === 'not_waiting'
            ? 'This question is no longer waiting for an answer.'
            : msg,
        );
      })
      .finally(() => setSending(false));
  };

  return (
    <form className="hv-wf-human" onSubmit={submit} aria-label="Answer">
      <p className="hv-wf-prompt">{plainText(waiting.prompt)}</p>
      <SchemaForm
        schema={schema}
        value={draft}
        onChange={(v) => setDraft(id, v)}
      />
      {problem ? (
        <p className="hv-wf-error" role="alert">
          {plainText(problem)}
        </p>
      ) : null}
      <div className="hv-wf-actions">
        <button
          type="submit"
          className="hv-button"
          data-kind="primary"
          disabled={sending}
        >
          <span className="hv-button__label">Answer</span>
        </button>
      </div>
    </form>
  );
}
