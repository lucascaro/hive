// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// ---------- Workflows (spec 530) ----------
//
// One blocking `xl` modal for a project's workflows: the list, the trust
// view, review-and-start, and a run. The engine is the `workflows` plugin;
// everything is a request over PLUGIN_MESSAGE (app/workflows.ts). Every
// string that came from a workflow file, an agent or the engine renders as
// React text through lib/workflow-text.ts, never as markup.
//
// The trust view is a view in this modal, not a stacked dialog: its
// y/n/Esc keys live in the `workflow-trust` key scope, which is active
// only while this view is up (no text input is on screen then). Enter is
// native: it activates the focused button, and focus starts on Cancel.

import {
  type FormEvent,
  type ReactNode,
  useCallback,
  useEffect,
  useLayoutEffect,
  useState,
} from 'react';
import { useShortcutLabel } from '../../app/bindings.js';
import { openChoiceDialog } from '../../app/modals/choice-dialog.js';
import {
  BUILD_TIMEOUT_MS,
  WorkflowError,
  closeWorkflows,
  loadRunState,
  rememberRuns,
  request,
  selectNode,
  setTrustConfirm,
  startWorkflowsPlugin,
  workflowsBack,
} from '../../app/workflows.js';
import type { PluginInfo } from '../../app/state.js';
import { describe, initialRaw, toValue } from '../../lib/schema-form.js';
import { displayRunState, waitingKeys } from '../../lib/workflow-overlay.js';
import {
  ago,
  formatBytes,
  plainText,
  showControls,
} from '../../lib/workflow-text.js';
import type {
  BuildReply,
  RunSummary,
  TrustDescribe,
  WorkflowsList,
} from '../../lib/workflow-types.js';
import { useAppStore } from '../../store/store.js';
import {
  dropDraft,
  setDraft,
  setView,
  startDraftId,
  useWorkflows,
} from '../../store/workflows.js';
import { Button } from '../Button.js';
import { Kbd } from '../Kbd.js';
import { NodeDetail } from '../workflows/NodeDetail.js';
import { SchemaForm } from '../workflows/SchemaForm.js';
import { StateMark } from '../workflows/StateMark.js';
import { WorkflowGraph } from '../workflows/WorkflowGraph.js';
import { ModalShell, type ModalHint } from './ModalShell.js';

export function Workflows({ root }: { root: HTMLElement | null }): ReactNode {
  const entry = useAppStore((s) => s.modals.find((m) => m.id === 'workflows'));
  // #workflows sits outside React's tree: its open/closed class is applied
  // here as a layout effect so every keyboard gate sees it in the frame
  // the panel first paints (Worktrees.tsx does the same).
  useLayoutEffect(() => {
    root?.classList.toggle('hidden', !entry);
  }, [root, entry]);
  if (!entry || entry.id !== 'workflows' || !root) return null;
  return (
    <WorkflowsPanel
      key={entry.seq}
      root={root}
      projectId={entry.projectId}
      projectName={entry.projectName}
    />
  );
}

const CRUMB: Record<string, string> = {
  trust: 'Trust',
  review: 'Review',
  run: 'Run',
};

function WorkflowsPanel({
  root,
  projectId,
  projectName,
}: {
  root: HTMLElement;
  projectId: string;
  projectName: string;
}): ReactNode {
  const view = useWorkflows((s) => s.view);
  const plugin = useAppStore((s) =>
    s.plugins.find((p) => p.id === 'workflows'),
  );
  const hint = useShortcutLabel('workflows');
  const running = !!plugin && plugin.enabled && plugin.status === 'running';

  // Focus lands on the close button, as in every other dialog; the trust
  // view moves it to Cancel itself.
  useEffect(() => {
    document.getElementById('workflows-close')?.focus();
  }, []);

  // patterns.md > Keyboard hints: `[...]` for symbols, `(...)` for letters.
  const hints: ModalHint[] =
    running && view === 'trust'
      ? [
          { keys: '(y)', label: 'trust' },
          { keys: '(n)/[esc]', label: 'cancel' },
        ]
      : running && view !== 'list'
        ? [{ keys: '[esc]', label: 'back' }]
        : [{ keys: '[esc]', label: 'close' }];

  return (
    <ModalShell
      id="workflows"
      root={root}
      title="Workflows"
      titleSuffix={
        <>
          <span id="workflows-project">
            {projectName ? ` · ${projectName}` : ''}
            {running && CRUMB[view] ? ` › ${CRUMB[view]}` : ''}
          </span>
          {hint ? <Kbd>{hint}</Kbd> : null}
        </>
      }
      size="xl"
      onClose={closeWorkflows}
      onEscape={workflowsBack}
      hints={hints}
    >
      {!running ? (
        <PluginGate plugin={plugin} />
      ) : view === 'trust' ? (
        <TrustView projectId={projectId} />
      ) : view === 'review' ? (
        <ReviewView projectId={projectId} />
      ) : view === 'run' ? (
        <RunView />
      ) : (
        <ListView projectId={projectId} />
      )}
    </ModalShell>
  );
}

// ---------- plugin gate ----------

function PluginGate({ plugin }: { plugin: PluginInfo | undefined }): ReactNode {
  if (!plugin) {
    return (
      <Notice title="This Hive has no workflow engine.">
        Update Hive to use workflows.
      </Notice>
    );
  }
  if (plugin.status === 'refused') {
    return (
      <Notice
        title="The workflow engine cannot start."
        action={<Button label="Restart" onClick={startWorkflowsPlugin} />}
      >
        {plainText(plugin.status_detail ?? 'It refused to start.')}
      </Notice>
    );
  }
  if (plugin.status === 'failed' || plugin.status === 'crashed') {
    return (
      <Notice
        title="The workflow engine stopped."
        action={<Button label="Restart" onClick={startWorkflowsPlugin} />}
      >
        {plainText(plugin.status_detail ?? '')}
      </Notice>
    );
  }
  if (!plugin.enabled) {
    return (
      <Notice
        title="Workflows are off."
        action={
          <Button
            label="Turn on"
            kind="primary"
            onClick={startWorkflowsPlugin}
          />
        }
      >
        Workflows run agents on your project, one step at a time, from
        TypeScript files you review first.
      </Notice>
    );
  }
  return (
    <div className="hv-wf-center">
      <span className="phase-spinner" />
      <p>Starting the workflow engine...</p>
    </div>
  );
}

function Notice({
  title,
  children,
  action,
}: {
  title: string;
  children?: ReactNode;
  action?: ReactNode;
}): ReactNode {
  return (
    <div className="hv-wf-notice" role="status">
      <p className="hv-wf-notice__title">{title}</p>
      {children ? <p className="hv-wf-notice__body">{children}</p> : null}
      {action ?? null}
    </div>
  );
}

function ErrorLine({ error }: { error: unknown }): ReactNode {
  const msg = error instanceof Error ? error.message : String(error);
  return (
    <p className="hv-wf-error" role="alert">
      {plainText(msg)}
    </p>
  );
}

// ---------- list ----------

function ListView({ projectId }: { projectId: string }): ReactNode {
  const [list, setList] = useState<WorkflowsList | null>(null);
  const [runs, setRuns] = useState<RunSummary[]>([]);
  const [error, setError] = useState<unknown>(null);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let live = true;
    setError(null);
    request<WorkflowsList>('workflows.list', { project_id: projectId })
      .then((l) => live && setList(l))
      .catch((e: unknown) => live && setError(e));
    request<{ runs: RunSummary[] }>('runs.list', {
      project_id: projectId,
      limit: 20,
    })
      .then((r) => {
        rememberRuns(r.runs);
        if (live) setRuns(r.runs);
      })
      .catch(() => {
        /* the recent-runs list is a convenience; workflows.list reports */
      });
    return () => {
      live = false;
    };
  }, [projectId, tick]);

  const usable = list?.trust === 'trusted';
  return (
    <div className="hv-wf-list">
      <div className="hv-wf-list__head">
        <div>
          <span className="hv-wf-folder">
            {list ? showControls(list.folder) : ''}
          </span>
          {list ? (
            <span className="hv-wf-chip" data-trust={list.trust}>
              {trustWord(list.trust)}
            </span>
          ) : null}
        </div>
        <Button label="Refresh" onClick={() => setTick(tick + 1)} />
      </div>
      {error ? <ErrorLine error={error} /> : null}
      {!list && !error ? (
        <div className="hv-wf-center">
          <span className="phase-spinner" />
        </div>
      ) : null}
      {list?.trust === 'invalid' ? (
        <p className="hv-wf-error" role="alert">
          {plainText(
            list.invalid_reason ?? 'The workflows folder is not usable.',
          )}
        </p>
      ) : null}
      {list && !usable && list.trust !== 'invalid' ? (
        <div className="hv-wf-actions">
          <Button
            label="Review & trust…"
            kind="primary"
            onClick={() => setView('trust', { file: null })}
          />
        </div>
      ) : null}
      {list ? (
        <ul className="hv-wf-files" aria-label="Workflows">
          {list.workflows.length === 0 ? (
            <li className="hv-wf-muted">No workflow files in this folder.</li>
          ) : null}
          {list.workflows.map((w) => (
            <li key={w.file}>
              <button
                type="button"
                className="hv-wf-row"
                disabled={!usable}
                onClick={() => setView('review', { file: w.file })}
              >
                <span className="hv-wf-row__main">{showControls(w.file)}</span>
                <span className="hv-wf-row__sub">
                  {w.name ? plainText(w.name) : '(not built yet)'}
                </span>
              </button>
            </li>
          ))}
        </ul>
      ) : null}
      <h5 className="hv-wf-section">Recent runs</h5>
      {runs.length === 0 ? (
        <p className="hv-wf-muted">No runs yet.</p>
      ) : (
        <ul className="hv-wf-files" aria-label="Recent runs">
          {runs.map((r) => (
            <li key={r.run_id}>
              <button
                type="button"
                className="hv-wf-row"
                data-run-id={r.run_id}
                onClick={() => {
                  void loadRunState(r.run_id);
                  setView('run', { runId: r.run_id, selectedKey: null });
                }}
              >
                <StateMark state={r.state} />
                <span className="hv-wf-row__main">{plainText(r.workflow)}</span>
                <span className="hv-wf-row__sub">
                  {ago(r.created_at)}
                  {r.source_changed ? ' (source changed)' : ''}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function trustWord(t: WorkflowsList['trust']): string {
  switch (t) {
    case 'trusted':
      return 'trusted';
    case 'changed':
      return 'changed since trusted';
    case 'invalid':
      return 'invalid';
    default:
      return 'not trusted';
  }
}

// ---------- trust ----------

type TrustLoad =
  | { status: 'loading' }
  | { status: 'ready'; d: TrustDescribe }
  | { status: 'error'; code: string; message: string };

function TrustView({ projectId }: { projectId: string }): ReactNode {
  const file = useWorkflows((s) => s.file);
  const [load, setLoad] = useState<TrustLoad>({ status: 'loading' });
  const [notice, setNotice] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [tick, setTick] = useState(0);

  useEffect(() => {
    let live = true;
    setLoad({ status: 'loading' });
    request<TrustDescribe>('workflow.trust.describe', { project_id: projectId })
      .then((d) => live && setLoad({ status: 'ready', d }))
      .catch((e: unknown) => {
        if (!live) return;
        setLoad({
          status: 'error',
          code: e instanceof WorkflowError ? e.code : 'error',
          message: e instanceof Error ? e.message : String(e),
        });
      });
    return () => {
      live = false;
    };
  }, [projectId, tick]);

  // Focus starts on Cancel: Enter must never trust by reflex.
  useEffect(() => {
    document.getElementById('workflow-trust-cancel')?.focus();
  }, []);

  const hash = load.status === 'ready' ? load.d.hash : '';
  const confirm = useCallback(() => {
    if (!hash || busy) return;
    setBusy(true);
    request('workflow.trust.confirm', { project_id: projectId, hash })
      .then(() =>
        file ? setView('review', { file }) : setView('list', { file: null }),
      )
      .catch((e: unknown) => {
        if (e instanceof WorkflowError && e.code === 'changed') {
          setNotice(
            'The files changed while you were reading. Review them again.',
          );
          setTick((t) => t + 1);
        } else {
          setLoad({
            status: 'error',
            code: e instanceof WorkflowError ? e.code : 'error',
            message: e instanceof Error ? e.message : String(e),
          });
        }
      })
      .finally(() => setBusy(false));
  }, [hash, busy, projectId, file]);

  // The `y` key (scope workflow-trust) reaches confirm through here.
  useEffect(() => {
    setTrustConfirm(load.status === 'ready' ? confirm : null);
    return () => setTrustConfirm(null);
  }, [load.status, confirm]);

  return (
    <div className="hv-wf-trust">
      {load.status === 'loading' ? (
        <div className="hv-wf-center">
          <span className="phase-spinner" />
        </div>
      ) : null}
      {notice ? (
        <p className="hv-wf-error" role="alert">
          {notice}
        </p>
      ) : null}
      {load.status === 'error' ? (
        <p className="hv-wf-error" role="alert">
          {trustProblem(load.code, load.message)}
        </p>
      ) : null}
      {load.status === 'ready' ? (
        <>
          <p className="hv-wf-trust__q">
            Trust the workflows in <code>{showControls(load.d.folder)}</code>?
          </p>
          <p className="hv-wf-muted">
            Contents <code>{load.d.hash.slice(0, 12)}</code>
          </p>
          <ul className="hv-wf-trust__files" aria-label="Files">
            {load.d.files.map((f) => (
              <li key={f.path}>
                <span>{showControls(f.path)}</span>
                <span className="hv-wf-muted">{formatBytes(f.bytes)}</span>
              </li>
            ))}
          </ul>
          <p>
            Building a workflow runs these files as you, inside Node's
            permission sandbox. The sandbox is defence in depth, not a security
            boundary. Hive asks again whenever any file here changes. Nothing
            has been built yet.
          </p>
        </>
      ) : null}
      <div className="hv-wf-actions">
        <Button
          id="workflow-trust-cancel"
          label="Cancel"
          onClick={workflowsBack}
        />
        {load.status === 'ready' ? (
          <Button
            id="workflow-trust-confirm"
            label="Trust and continue"
            kind="danger"
            disabled={busy}
            onClick={confirm}
          />
        ) : null}
      </div>
    </div>
  );
}

function trustProblem(code: string, message: string): string {
  switch (code) {
    case 'symlink':
      return 'A file in the workflows folder is a symbolic link. Hive will not trust folders that contain one.';
    case 'too_large':
      return 'The workflows folder is too large to review here.';
    case 'missing':
      return 'There is no workflows folder in this project.';
    default:
      return plainText(message);
  }
}

// ---------- review & start ----------

type Build =
  | { status: 'loading' }
  | { status: 'ready'; b: BuildReply }
  | { status: 'error'; e: WorkflowError | Error };

function ReviewView({ projectId }: { projectId: string }): ReactNode {
  const file = useWorkflows((s) => s.file) ?? '';
  const [build, setBuild] = useState<Build>({ status: 'loading' });
  const [slow, setSlow] = useState(false);
  const [notice, setNotice] = useState<string | null>(null);
  const [tick, setTick] = useState(0);
  const [problem, setProblem] = useState<string | null>(null);
  const [starting, setStarting] = useState(false);

  useEffect(() => {
    let live = true;
    setBuild({ status: 'loading' });
    setSlow(false);
    const t = setTimeout(() => live && setSlow(true), 5000);
    request<BuildReply>(
      'workflow.build',
      { project_id: projectId, file },
      { timeoutMs: BUILD_TIMEOUT_MS },
    )
      .then((b) => live && setBuild({ status: 'ready', b }))
      .catch((e: unknown) => {
        if (!live) return;
        if (
          e instanceof WorkflowError &&
          (e.code === 'untrusted' || e.code === 'changed')
        ) {
          setView('trust', { file });
          return;
        }
        setBuild({
          status: 'error',
          e: e instanceof Error ? e : new Error(String(e)),
        });
      })
      .finally(() => clearTimeout(t));
    return () => {
      live = false;
      clearTimeout(t);
    };
  }, [projectId, file, tick]);

  const b = build.status === 'ready' ? build.b : null;
  const draftId = b ? startDraftId(projectId, file, b.source_hash) : '';
  const draft = useWorkflows((s) => (draftId ? s.drafts[draftId] : undefined));

  const start = (ev: FormEvent) => {
    ev.preventDefault();
    if (!b) return;
    const { value, errors } = toValue(
      b.input_schema,
      draft ?? initialRaw(describe(b.input_schema)),
    );
    if (errors.length) {
      setProblem(errors.join('\n'));
      return;
    }
    setProblem(null);
    setStarting(true);
    request<{ run_id: string }>('run.start', {
      project_id: projectId,
      file,
      input: value,
      source_hash: b.source_hash,
    })
      .then((r) => {
        dropDraft(draftId);
        void loadRunState(r.run_id);
        setView('run', { runId: r.run_id, selectedKey: null });
      })
      .catch((e: unknown) => {
        if (e instanceof WorkflowError && e.code === 'changed') {
          setNotice('The workflow changed since you reviewed it.');
          setTick((t) => t + 1);
        } else {
          setProblem(e instanceof Error ? e.message : String(e));
        }
      })
      .finally(() => setStarting(false));
  };

  if (build.status === 'loading') {
    return (
      <div className="hv-wf-center">
        <span className="phase-spinner" />
        <p>
          Building {showControls(file)}
          {slow ? '. The first build may download the type checker.' : '...'}
        </p>
      </div>
    );
  }
  if (build.status === 'error') {
    return <BuildError e={build.e} />;
  }
  return (
    <form
      className="hv-wf-review"
      onSubmit={start}
      aria-label="Review and start"
    >
      <div className="hv-wf-review__graph">
        {notice ? (
          <p className="hv-wf-error" role="alert">
            {notice}
          </p>
        ) : null}
        <h4 className="hv-wf-section">{plainText(build.b.name)}</h4>
        <WorkflowGraph ir={build.b.ir} />
      </div>
      <div className="hv-wf-review__form">
        <h5 className="hv-wf-section">Input</h5>
        <SchemaForm
          schema={build.b.input_schema}
          value={draft}
          onChange={(v) => setDraft(draftId, v)}
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
            disabled={starting}
          >
            <span className="hv-button__label">Start run</span>
          </button>
        </div>
      </div>
    </form>
  );
}

function BuildError({ e }: { e: Error }): ReactNode {
  const we = e instanceof WorkflowError ? e : null;
  if (we?.type_errors?.length) {
    return (
      <div className="hv-wf-build-errors">
        <h5 className="hv-wf-section">Type errors</h5>
        <ul>
          {we.type_errors.map((t) => (
            <li key={`${t.file}:${t.line}:${t.col}:${t.message}`}>
              <code>
                {showControls(t.file)}:{t.line}:{t.col}
              </code>{' '}
              {plainText(t.message)}
            </li>
          ))}
        </ul>
      </div>
    );
  }
  if (we?.errors?.length) {
    return (
      <div className="hv-wf-build-errors">
        <h5 className="hv-wf-section">
          {we.code === 'invalid_ir' ? 'Invalid workflow' : 'Build failed'}
        </h5>
        <ul>
          {we.errors.map((m) => (
            <li key={m}>{plainText(m)}</li>
          ))}
        </ul>
      </div>
    );
  }
  return <ErrorLine error={e} />;
}

// ---------- run ----------

function RunView(): ReactNode {
  const runId = useWorkflows((s) => s.runId);
  const run = useWorkflows((s) => s.run);
  const selectedKey = useWorkflows((s) => s.selectedKey);

  useEffect(() => {
    if (runId && run?.runId !== runId) void loadRunState(runId);
  }, [runId, run?.runId]);

  // A run that opens on a wait shows the node that is waiting.
  const waiting =
    run?.status === 'ready' ? (waitingKeys(run.overlay)[0] ?? null) : null;
  useEffect(() => {
    if (waiting && !selectedKey) selectNode(waiting);
  }, [waiting, selectedKey]);

  if (!runId || !run || run.runId !== runId || run.status === 'loading') {
    return (
      <div className="hv-wf-center">
        <span className="phase-spinner" />
        <p>Loading the run...</p>
      </div>
    );
  }
  if (run.status === 'error' || !run.detail || !run.ir) {
    return (
      <div className="hv-wf-notice">
        <ErrorLine error={run.error ?? 'Could not load the run.'} />
        <Button label="Try again" onClick={() => void loadRunState(runId)} />
      </div>
    );
  }
  const { detail, ir, overlay } = run;
  const state = displayRunState(overlay) ?? detail.state;
  const active = state === 'running' || state === 'waiting';

  const cancel = async () => {
    const choice = await openChoiceDialog({
      title: 'Cancel this run?',
      detail: detail.workflow,
      note: 'Running agents are stopped. Worktrees with changes are kept.',
      choices: [
        { label: 'Keep running', value: 'keep' },
        { label: 'Cancel run', value: 'cancel', danger: true },
      ],
    });
    if (choice === 'cancel') {
      request('run.cancel', { run_id: runId }).catch(() => {
        /* the run's own events report how it ended */
      });
    }
  };

  return (
    <div className="hv-wf-run">
      <header className="hv-wf-run__head">
        <StateMark state={state} />
        <span className="hv-wf-run__name">{plainText(detail.workflow)}</span>
        <span className="hv-wf-muted">started {ago(detail.created_at)}</span>
        {detail.source_changed ? (
          <span className="hv-wf-chip" data-trust="changed">
            source changed since this run started
          </span>
        ) : null}
        <span className="hv-wf-run__spacer" />
        {active ? <Button label="Cancel run" onClick={cancel} /> : null}
        {state === 'failed' || state === 'cancelled' ? (
          <Button
            label="Retry"
            onClick={() => {
              request('run.retry', { run_id: runId }).catch(() => {});
            }}
          />
        ) : null}
      </header>
      {detail.error ? (
        <p className="hv-wf-error" role="alert">
          {plainText(detail.error)}
        </p>
      ) : null}
      {detail.kept_worktrees?.length ? (
        <p className="hv-wf-muted">
          Kept worktrees with changes:{' '}
          {detail.kept_worktrees.map((w) => showControls(w)).join(', ')}
          {detail.kept_worktrees_total &&
          detail.kept_worktrees_total > detail.kept_worktrees.length
            ? ` and ${detail.kept_worktrees_total - detail.kept_worktrees.length} more`
            : ''}
        </p>
      ) : null}
      <div className="hv-wf-run__body">
        <div className="hv-wf-run__graph">
          <WorkflowGraph
            ir={ir}
            overlay={overlay}
            selectedKey={selectedKey}
            onSelect={selectNode}
          />
        </div>
        <div className="hv-wf-run__detail">
          {selectedKey ? (
            <NodeDetail
              runId={runId}
              nodeKey={selectedKey}
              ir={ir}
              overlay={overlay}
            />
          ) : (
            <p className="hv-wf-muted">Select a node to see its details.</p>
          )}
        </div>
      </div>
    </div>
  );
}
