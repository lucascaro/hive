// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { type AcpAgentInfo, catalog } from '../ir/catalog.ts';
import { s } from '../sdk/schema.ts';
import { workflow } from '../sdk/dsl.ts';
import { examples } from '../tools/examples.ts';

// The shape of an ACP_AGENTS reply (internal/wire.AcpAgentInfo) as the
// daemon builds it from agent.acpSpecs, with the user's ceilings raised to
// what the library examples use (Pi at "unattended", or it is unavailable). Gemini is the experimental, no-modes entry
// the daemon marks unavailable; aider is absent because it has no ACP route.
const agents: AcpAgentInfo[] = JSON.parse(readFileSync(join(dirname(fileURLToPath(import.meta.url)), 'fixtures/acp-agents.json'), 'utf8'));

const one = (agent: string, mode?: string) =>
  workflow('one', s.object({ task: s.string() }), (g) => {
    g.group('grp', (b) => {
      b.agent('a', { agent, ...(mode === undefined ? {} : { mode }), prompt: 'x', output: s.object({ ok: s.boolean() }) });
    });
  });

test('accepts the three library IRs under the fixture catalog', () => {
  for (const [name, make] of Object.entries(examples)) assert.deepEqual(catalog(make(), agents), [], name);
});

test('accepts an omitted mode', () => {
  assert.deepEqual(catalog(one('claude'), agents), []);
});

test('rejects agent with no ACP route (aider)', () => {
  assert.deepEqual(catalog(one('aider'), agents), ['grp/a: agent "aider" has no ACP route']);
});

test('rejects experimental (gemini)', () => {
  assert.match(catalog(one('gemini'), agents)[0], /grp\/a: agent "gemini" is experimental/);
});

test('rejects mode not in catalog', () => {
  assert.equal(
    catalog(one('codex', 'plan'), agents)[0],
    'grp/a: mode "plan" is not one of read-only|workspace-write|agent|agent-full-access for agent "codex"',
  );
});

test('rejects mode above ceiling', () => {
  assert.equal(catalog(one('claude', 'bypassPermissions'), agents)[0], 'grp/a: mode "bypassPermissions" is above the "acceptEdits" ceiling set for agent "claude"');
  assert.deepEqual(catalog(one('claude', 'acceptEdits'), agents), []);
  assert.deepEqual(catalog(one('claude', 'plan'), agents), []);
});

test('rejects unavailable agent with reason', () => {
  const reason = 'Pi never asks before using a tool; allow unattended tool use for it in Settings → Agents to use ACP';
  const pi = agents.map((a) => (a.id === 'pi' ? { ...a, ceiling: 'off', available: false, reason } : a));
  const out = catalog(one('pi', 'off'), pi);
  assert.match(out[0], /^grp\/a: agent "pi" is unavailable: Pi never asks before using a tool; allow unattended/);
});

test('reports every failing node, nested in groups and loops', () => {
  const ir = examples.reviewLoop();
  const errs = catalog(ir, agents.filter((a) => a.id !== 'codex'));
  assert.equal(errs.length, 1);
  assert.match(errs[0], /agent "codex" has no ACP route/);
});

test('a reply without a ceiling falls back to the default mode', () => {
  const noCeiling = agents.map(({ ceiling: _c, ...rest }) => rest);
  assert.match(catalog(one('claude', 'acceptEdits'), noCeiling)[0], /above the "default" ceiling/);
});
