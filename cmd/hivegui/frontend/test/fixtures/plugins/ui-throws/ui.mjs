// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

const boom = () => {
  throw new Error('ui-throws: render boom');
};
export default function activate() {
  return {
    badge: boom,
    sessionView: {
      banner: boom,
      panel: { title: 'Throws', component: boom },
    },
    settings: boom,
    commands: [{ id: 'boom', title: 'Throw from a command', run: boom }],
  };
}
