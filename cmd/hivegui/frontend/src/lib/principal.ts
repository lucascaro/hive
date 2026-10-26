// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Provenance principals (spec 528). Mirrors wire.ParsePrincipal in
// internal/wire/request.go, the one Go parser: keep the two in step.

const RUN_ID = /^[a-z0-9][a-z0-9-]{0,62}$/;
const SEP = '/workflow:';

export interface Principal {
  /** The plugin id, '' for any other principal (the user, a session…). */
  plugin: string;
  /** The workflow run id, '' for a plain plugin principal. */
  run: string;
}

/** Splits "plugin:<id>" or "plugin:<id>/workflow:<run>". */
export function parsePrincipal(s: string): Principal {
  const none = { plugin: '', run: '' };
  if (!s.startsWith('plugin:')) return none;
  const rest = s.slice('plugin:'.length);
  const i = rest.indexOf(SEP);
  const id = i < 0 ? rest : rest.slice(0, i);
  if (id === '' || /[/:]/.test(id)) return none;
  if (i < 0) return { plugin: id, run: '' };
  const run = rest.slice(i + SEP.length);
  return RUN_ID.test(run) ? { plugin: id, run } : none;
}
