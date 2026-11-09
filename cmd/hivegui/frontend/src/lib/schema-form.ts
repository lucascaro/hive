// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Form descriptors and coercion for the workflow engine's JSON Schema
// subset (plugins/workflows/sdk/schema.ts). The engine validates
// authoritatively (`invalid_answer`, `run.start` errors); this stops at
// what the native input types can express, so there is no second
// validator to drift.

import type { JsonSchema } from './workflow-types.js';

export type FieldKind =
  | 'string'
  | 'enum'
  | 'number'
  | 'boolean'
  | 'list' // array of scalars, one per line
  | 'json' // array of objects (or anything else), JSON text
  | 'object';

export interface Field {
  name: string;
  kind: FieldKind;
  required: boolean;
  options?: string[];
  /** The scalar type of a `list`'s items. */
  item?: 'string' | 'number' | 'boolean';
  fields?: Field[];
}

/** What a form holds while the user types: text for every input except
 * checkboxes, nested for objects. Kept in the store so a draft survives the
 * modal closing. */
export type Raw = string | boolean | { [name: string]: Raw };

type ObjectSchema = Extract<JsonSchema, { type: 'object' }>;

const isObject = (s: JsonSchema): s is ObjectSchema => s.type === 'object';

function describeOne(name: string, s: JsonSchema, required: boolean): Field {
  const t = (s as { type: string }).type;
  switch (t) {
    case 'string': {
      const e = (s as { enum?: string[] }).enum;
      return e?.length
        ? { name, kind: 'enum', required, options: e }
        : { name, kind: 'string', required };
    }
    case 'number':
    case 'integer':
      return { name, kind: 'number', required };
    case 'boolean':
      return { name, kind: 'boolean', required };
    case 'array': {
      const items = (s as { items: JsonSchema }).items;
      const it = (items as { type: string }).type;
      if (
        it === 'string' ||
        it === 'number' ||
        it === 'integer' ||
        it === 'boolean'
      ) {
        return {
          name,
          kind: 'list',
          required,
          item:
            it === 'integer'
              ? 'number'
              : (it as 'string' | 'number' | 'boolean'),
        };
      }
      return { name, kind: 'json', required };
    }
    case 'object':
      return {
        name,
        kind: 'object',
        required,
        fields: fieldsOf(s as ObjectSchema),
      };
    default:
      return { name, kind: 'json', required };
  }
}

/** The fields of an object schema, in property order. */
export function fieldsOf(schema: ObjectSchema): Field[] {
  const req = new Set(schema.required ?? []);
  return Object.entries(schema.properties ?? {}).map(([name, s]) =>
    describeOne(name, s, req.has(name)),
  );
}

/** Fields for any schema: a non-object root becomes one JSON field. */
export function describe(schema: JsonSchema): Field[] {
  return isObject(schema)
    ? fieldsOf(schema)
    : [describeOne('value', schema, true)];
}

export function initialRaw(fields: Field[]): { [name: string]: Raw } {
  const out: { [name: string]: Raw } = {};
  for (const f of fields) {
    out[f.name] =
      f.kind === 'boolean'
        ? false
        : f.kind === 'object'
          ? initialRaw(f.fields ?? [])
          : '';
  }
  return out;
}

export interface Coerced {
  value: unknown;
  errors: string[];
}

function scalar(item: 'string' | 'number' | 'boolean', line: string): unknown {
  if (item === 'number') return Number(line);
  if (item === 'boolean') return line.trim().toLowerCase() === 'true';
  return line;
}

function coerceFields(
  fields: Field[],
  raw: { [name: string]: Raw },
  path: string,
  errors: string[],
): Record<string, unknown> {
  const out: Record<string, unknown> = {};
  for (const f of fields) {
    const r = raw[f.name];
    const where = path ? `${path}.${f.name}` : f.name;
    switch (f.kind) {
      case 'boolean':
        out[f.name] = r === true;
        break;
      case 'object': {
        const nested = coerceFields(
          f.fields ?? [],
          typeof r === 'object' ? r : {},
          where,
          errors,
        );
        if (f.required || Object.keys(nested).length > 0) out[f.name] = nested;
        break;
      }
      case 'number': {
        const t = typeof r === 'string' ? r.trim() : '';
        if (t === '') break;
        const n = Number(t);
        if (Number.isNaN(n)) errors.push(`${where}: not a number`);
        else out[f.name] = n;
        break;
      }
      case 'list': {
        const lines = (typeof r === 'string' ? r : '')
          .split('\n')
          .map((l) => l.replace(/\r$/, ''))
          .filter((l) => l.trim() !== '');
        if (lines.length === 0 && !f.required) break;
        const vals = lines.map((l) => scalar(f.item ?? 'string', l));
        if (f.item === 'number' && vals.some((v) => Number.isNaN(v)))
          errors.push(`${where}: every line must be a number`);
        out[f.name] = vals;
        break;
      }
      case 'json': {
        const t = typeof r === 'string' ? r.trim() : '';
        if (t === '') {
          if (f.required) out[f.name] = [];
          break;
        }
        try {
          out[f.name] = JSON.parse(t);
        } catch {
          errors.push(`${where}: not valid JSON`);
        }
        break;
      }
      default: {
        const t = typeof r === 'string' ? r : '';
        if (t === '' && !f.required) break;
        out[f.name] = t;
      }
    }
  }
  return out;
}

/** Coerces a form's raw values to the schema's JSON value. Parse problems
 * come back in `errors`; everything else is the engine's to judge. */
export function toValue(
  schema: JsonSchema,
  raw: { [name: string]: Raw },
): Coerced {
  const errors: string[] = [];
  const fields = describe(schema);
  const obj = coerceFields(fields, raw, '', errors);
  return { value: isObject(schema) ? obj : obj.value, errors };
}
