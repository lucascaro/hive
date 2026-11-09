// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// A workflow node or run state: always an icon AND a word (principle 5).
// The icon set is the session state sprite plus three workflow-only
// shapes; colour and motion come from workflows.css via data-mark.

import type { ReactNode } from 'react';
import type { NodeState, RunState } from '../../lib/workflow-types.js';
import { Icon, type IconName } from '../Icon.js';

type Mark = {
  icon: IconName;
  /** Drives colour and motion in workflows.css. */
  mark: string;
};

function markOf(state: NodeState | RunState, permission: boolean): Mark {
  switch (state) {
    case 'running':
      return { icon: 'state-working', mark: 'running' };
    case 'waiting':
      return permission
        ? { icon: 'state-waiting-permission', mark: 'attention' }
        : { icon: 'state-attention', mark: 'attention' };
    case 'done':
      return { icon: 'check', mark: 'done' };
    case 'failed':
      return { icon: 'state-failed', mark: 'failed' };
    case 'cancelled':
      return { icon: 'state-exited', mark: 'exited' };
    case 'skipped':
      return { icon: 'state-skipped', mark: 'muted' };
    case 'interrupted':
      return { icon: 'state-interrupted', mark: 'failed' };
    default:
      return { icon: 'state-pending', mark: 'muted' };
  }
}

export function stateWord(
  state: NodeState | RunState,
  permission = false,
): string {
  if (state === 'waiting')
    return permission ? 'waiting for permission' : 'waiting for you';
  return state;
}

export function StateMark({
  state,
  permission = false,
  wordless = false,
}: {
  state: NodeState | RunState;
  /** A waiting node that waits on a permission, not on an answer. */
  permission?: boolean;
  wordless?: boolean;
}): ReactNode {
  const { icon, mark } = markOf(state, permission);
  return (
    <span className="hv-wf-mark" data-mark={mark} data-state={state}>
      <Icon name={icon} size={14} />
      {wordless ? null : (
        <span className="hv-wf-mark__word">{stateWord(state, permission)}</span>
      )}
    </span>
  );
}
