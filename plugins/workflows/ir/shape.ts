// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Structural check of unknown JSON as a workflow IR (spec 529). validate.ts
// assumes the IR has the right shape, which holds for what the builder
// returns but not for a stored IR or one a build child printed. This is the
// same set of rules as ir.schema.json (kinds, required keys, no extra keys,
// types, id grammar) plus the limits the schema cannot say: result-schema
// size and depth, and a bound on how deeply nodes and conditions nest.

import { MAX_CONCURRENCY, MAX_ITERS } from '../sdk/dsl.ts';
import { checkSchema } from './resultschema.ts';

export const MAX_NESTING = 32;

const ID = /^[a-z][a-z0-9]*(-[a-z0-9]+)*(\/[a-z][a-z0-9]*(-[a-z0-9]+)*)*$/;
const OPS = ['eq', 'ne', 'in', 'truthy', 'lt', 'gt'];

const KEYS: Record<string, readonly string[]> = {
  agent: ['id', 'kind', 'agent', 'mode', 'worktree', 'timeout_s', 'prompt', 'output'],
  check: ['id', 'kind', 'argv', 'timeout_s'],
  human: ['id', 'kind', 'prompt', 'output'],
  group: ['id', 'kind', 'nodes', 'edges'],
  loop: ['id', 'kind', 'max_iters', 'until', 'nodes', 'edges'],
  map: ['id', 'kind', 'over', 'concurrency', 'nodes', 'edges'],
};
const OPTIONAL = new Set(['mode']);

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);
const isPosNum = (v: unknown): boolean => typeof v === 'number' && Number.isFinite(v) && v > 0;
const isIntIn = (v: unknown, lo: number, hi: number): boolean => Number.isInteger(v) && (v as number) >= lo && (v as number) <= hi;

/** Every shape error in `json` as an IR; an empty list means validate() may run. */
export function shape(json: unknown): string[] {
  const errs: string[] = [];
  const err = (path: string, why: string): void => {
    errs.push(`${path}: ${why}`);
  };

  const exactKeys = (v: Record<string, unknown>, path: string, allowed: readonly string[], required: readonly string[]) => {
    for (const k of Object.keys(v)) if (!allowed.includes(k)) err(path, `unexpected key "${k}"`);
    for (const k of required) if (!(k in v)) err(path, `missing key "${k}"`);
  };

  const schema = (v: unknown, path: string) => {
    const why = checkSchema(v, path);
    if (why) errs.push(why);
  };

  const jsonValue = (v: unknown, path: string, depth: number): void => {
    if (depth > MAX_NESTING) return err(path, `nested deeper than ${MAX_NESTING} levels`);
    if (v === null || typeof v === 'string' || typeof v === 'boolean') return;
    if (typeof v === 'number') return void (Number.isFinite(v) || err(path, 'not a finite number'));
    if (Array.isArray(v)) return v.forEach((x, i) => jsonValue(x, `${path}[${i}]`, depth + 1));
    if (isRecord(v)) return Object.entries(v).forEach(([k, x]) => jsonValue(x, `${path}.${k}`, depth + 1));
    err(path, 'not a JSON value');
  };

  const cond = (v: unknown, path: string, depth: number): void => {
    if (depth > MAX_NESTING) return err(path, `condition nested deeper than ${MAX_NESTING} levels`);
    if (!isRecord(v)) return err(path, 'must be an object');
    if ('all' in v || 'any' in v) {
      const k = 'all' in v ? 'all' : 'any';
      exactKeys(v, path, [k], [k]);
      const list = v[k];
      if (!Array.isArray(list)) return err(`${path}.${k}`, 'must be an array');
      return list.forEach((c, i) => cond(c, `${path}.${k}[${i}]`, depth + 1));
    }
    if ('not' in v) {
      exactKeys(v, path, ['not'], ['not']);
      return cond(v.not, `${path}.not`, depth + 1);
    }
    exactKeys(v, path, ['ref', 'op', 'value'], ['ref', 'op']);
    if (typeof v.ref !== 'string') err(`${path}.ref`, 'must be a string');
    if (typeof v.op !== 'string' || !OPS.includes(v.op)) err(`${path}.op`, `must be one of ${OPS.join('|')}`);
    if ('value' in v) jsonValue(v.value, `${path}.value`, depth + 1);
  };

  const id = (v: unknown, path: string) => {
    if (typeof v !== 'string' || !ID.test(v)) err(path, 'must be an id of [a-z0-9] words joined by single hyphens and slashes');
  };

  const body = (v: Record<string, unknown>, path: string, depth: number) => {
    if (Array.isArray(v.nodes)) v.nodes.forEach((n, i) => node(n, `${path}.nodes[${i}]`, depth + 1));
    else err(`${path}.nodes`, 'must be an array');
    if (Array.isArray(v.edges)) {
      v.edges.forEach((e, i) => {
        const p = `${path}.edges[${i}]`;
        if (!isRecord(e)) return err(p, 'must be an object');
        exactKeys(e, p, ['from', 'to', 'when'], ['from', 'to']);
        id(e.from, `${p}.from`);
        id(e.to, `${p}.to`);
        if ('when' in e) cond(e.when, `${p}.when`, 1);
      });
    } else err(`${path}.edges`, 'must be an array');
  };

  const node = (v: unknown, path: string, depth: number): void => {
    if (depth > MAX_NESTING) return err(path, `nested deeper than ${MAX_NESTING} levels`);
    if (!isRecord(v)) return err(path, 'must be an object');
    const kind = v.kind;
    if (typeof kind !== 'string' || !Object.hasOwn(KEYS, kind)) return err(`${path}.kind`, 'must be agent, check, human, group, loop or map');
    const at = typeof v.id === 'string' ? v.id : path;
    exactKeys(v, at, KEYS[kind], KEYS[kind].filter((k) => !OPTIONAL.has(k)));
    id(v.id, `${path}.id`);
    switch (kind) {
      case 'agent':
        if (typeof v.agent !== 'string' || v.agent === '') err(`${at}.agent`, 'must be a non-empty string');
        if ('mode' in v && typeof v.mode !== 'string') err(`${at}.mode`, 'must be a string');
        if (v.worktree !== 'own') {
          if (isRecord(v.worktree)) {
            exactKeys(v.worktree, `${at}.worktree`, ['of'], ['of']);
            id(v.worktree.of, `${at}.worktree.of`);
          } else err(`${at}.worktree`, 'must be "own" or {of}');
        }
        if (!isPosNum(v.timeout_s)) err(`${at}.timeout_s`, 'must be a number > 0');
        if (typeof v.prompt !== 'string') err(`${at}.prompt`, 'must be a string');
        schema(v.output, `${at}.output`);
        break;
      case 'human':
        if (typeof v.prompt !== 'string') err(`${at}.prompt`, 'must be a string');
        schema(v.output, `${at}.output`);
        break;
      case 'check':
        if (!Array.isArray(v.argv) || v.argv.length === 0 || v.argv.some((a) => typeof a !== 'string')) {
          err(`${at}.argv`, 'must be a non-empty array of strings');
        }
        if (!isPosNum(v.timeout_s)) err(`${at}.timeout_s`, 'must be a number > 0');
        break;
      case 'loop':
        if (!isIntIn(v.max_iters, 1, MAX_ITERS)) err(`${at}.max_iters`, `must be an integer in 1..${MAX_ITERS}`);
        cond(v.until, `${at}.until`, 1);
        body(v, at, depth);
        break;
      case 'map':
        if (typeof v.over !== 'string') err(`${at}.over`, 'must be a string');
        if (!isIntIn(v.concurrency, 1, MAX_CONCURRENCY)) err(`${at}.concurrency`, `must be an integer in 1..${MAX_CONCURRENCY}`);
        body(v, at, depth);
        break;
      case 'group':
        body(v, at, depth);
        break;
    }
  };

  if (!isRecord(json)) return ['ir: must be an object'];
  exactKeys(json, 'ir', ['ir', 'name', 'input', 'nodes', 'edges'], ['ir', 'name', 'input', 'nodes', 'edges']);
  if (json.ir !== 1) err('ir.ir', 'must be 1');
  if (typeof json.name !== 'string') err('ir.name', 'must be a string');
  schema(json.input, 'ir.input');
  body(json, 'ir', 0);
  return errs;
}
