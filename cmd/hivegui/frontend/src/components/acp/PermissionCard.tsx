// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// An ACP permission request and its options (spec 496), shared by the
// session transcript and the workflow node detail (spec 530). Answers go
// through the existing ANSWER_PERMISSION; the title and option names are
// agent text and render as plain React text.
import { type ReactNode, useState } from 'react';

import { reportFailure } from '../../app/dom.js';
import { AnswerPermission } from '../../bridge.js';
import { type AcpPermission, isAllowOption } from '../../lib/acp.js';
import { Button } from '../Button.js';
import { Icon } from '../Icon.js';

export function PermissionCard({
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
