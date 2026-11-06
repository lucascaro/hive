// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The result-schema subset and its value validator (spec 529). A TypeScript
// port of the daemon's internal/acp/resultschema.go (spec 528): the same
// limits and the same messages, pinned by that package's shared fixtures
// (internal/acp/testdata/resultschema/*.json, see test/resultschema.test.ts).
// An agent node's `output`, a human node's `output` and the run `input` are
// all result schemas, so a valid IR never fails at CREATE_SESSION.

export const MAX_RESULT_SCHEMA_BYTES = 8 << 10;
export const MAX_RESULT_SCHEMA_DEPTH = 8;

const ALLOWED: Record<string, readonly string[]> = {
  string: ['type', 'enum'],
  number: ['type'],
  boolean: ['type'],
  array: ['type', 'items'],
  object: ['type', 'properties', 'required', 'additionalProperties'],
};

const isRecord = (v: unknown): v is Record<string, unknown> => typeof v === 'object' && v !== null && !Array.isArray(v);

/** Why `schema` is not an acceptable result schema, or null. `path` names it in messages. */
export function checkSchema(schema: unknown, path = 'schema'): string | null {
  let json: string | undefined;
  try {
    json = JSON.stringify(schema);
  } catch {
    return `${path}: not JSON`;
  }
  if (json === undefined) return `${path}: not JSON`;
  if (new TextEncoder().encode(json).length > MAX_RESULT_SCHEMA_BYTES) {
    return `${path}: larger than ${MAX_RESULT_SCHEMA_BYTES} bytes`;
  }
  const err = node(schema, path, 1);
  if (err) return err;
  if ((schema as { type: string }).type !== 'object') return `${path}: the top level must be an object schema`;
  return null;
}

function node(v: unknown, path: string, depth: number): string | null {
  if (depth > MAX_RESULT_SCHEMA_DEPTH) return `${path}: nested deeper than ${MAX_RESULT_SCHEMA_DEPTH} levels`;
  if (!isRecord(v)) return `${path}: must be an object`;
  const typ = v.type;
  const allowed = typeof typ === 'string' && Object.hasOwn(ALLOWED, typ) ? ALLOWED[typ] : null;
  if (!allowed) return `${path}: type must be string, number, boolean, array or object`;
  for (const k of Object.keys(v).sort()) {
    if (!allowed.includes(k)) return `${path}: "${k}" is not supported on a ${typ} schema`;
  }
  if (typ === 'string' && 'enum' in v) {
    const e = v.enum;
    if (!Array.isArray(e) || e.length === 0 || e.some((x) => typeof x !== 'string')) {
      return `${path}.enum: must be a non-empty array of strings`;
    }
  }
  if (typ === 'array') {
    if (!('items' in v)) return `${path}: an array schema needs items`;
    return node(v.items, `${path}.items`, depth + 1);
  }
  if (typ === 'object') {
    if (v.additionalProperties !== false) return `${path}: additionalProperties must be false`;
    if (!isRecord(v.properties)) return `${path}: an object schema needs properties`;
    if (!Array.isArray(v.required)) return `${path}: an object schema needs required`;
    if (v.required.some((r) => typeof r !== 'string')) return `${path}.required: must list property names`;
    const props = v.properties;
    const req = new Set(v.required as string[]);
    const names = Object.keys(props).sort();
    for (const name of names) {
      if (!req.has(name)) return `${path}.required: must list every property ("${name}" is missing)`;
    }
    for (const name of [...req].sort()) {
      if (!Object.hasOwn(props, name)) return `${path}.required: "${name}" is not a property`;
    }
    for (const name of names) {
      const err = node(props[name], `${path}.properties.${name}`, depth + 1);
      if (err) return err;
    }
  }
  return null;
}

function kind(v: unknown): string {
  if (v === null) return 'null';
  if (Array.isArray(v)) return 'array';
  return typeof v;
}

/** Why `value` does not match `schema` (already accepted by checkSchema), or null. */
export function checkValue(schema: unknown, value: unknown, path = 'data'): string | null {
  const s = schema as Record<string, unknown>;
  const got = kind(value);
  if (got !== s.type) return `${path}: expected ${s.type}, got ${got}`;
  switch (s.type) {
    case 'number':
      // JSON.parse turns 1e400 into Infinity, which the daemon calls out of range.
      if (!Number.isFinite(value)) return `${path}: ${String(value)} is out of range`;
      break;
    case 'string': {
      const e = s.enum as string[] | undefined;
      if (e && !e.includes(value as string)) return `${path}: ${JSON.stringify(value)} is not one of ${e.join('|')}`;
      break;
    }
    case 'array': {
      const items = value as unknown[];
      for (let i = 0; i < items.length; i++) {
        const err = checkValue(s.items, items[i], `${path}[${i}]`);
        if (err) return err;
      }
      break;
    }
    case 'object': {
      const m = value as Record<string, unknown>;
      const props = s.properties as Record<string, unknown>;
      for (const name of Object.keys(props).sort()) {
        if (!Object.hasOwn(m, name)) return `${path}.${name}: required field is missing`;
        const err = checkValue(props[name], m[name], `${path}.${name}`);
        if (err) return err;
      }
      const extra = Object.keys(m).filter((k) => !Object.hasOwn(props, k)).sort();
      if (extra.length > 0) return `${path}.${extra[0]}: unexpected field`;
      break;
    }
  }
  return null;
}
