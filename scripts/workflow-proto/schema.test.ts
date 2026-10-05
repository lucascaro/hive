import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { examples } from './examples.ts';
import { irSchema } from './ir-schema.ts';

const here = dirname(fileURLToPath(import.meta.url));

// A JSON Schema subset validator: exactly the keywords irSchema() uses. No
// dependency, so the check runs anywhere `node` does.
type Sch = Record<string, unknown>;
function check(root: Sch, schema: Sch, v: unknown, path: string, errs: string[]): void {
  if (typeof schema.$ref === 'string') {
    const name = schema.$ref.replace('#/$defs/', '');
    return check(root, (root.$defs as Record<string, Sch>)[name], v, path, errs);
  }
  if (Array.isArray(schema.oneOf)) {
    const ok = schema.oneOf.filter((s) => {
      const e: string[] = [];
      check(root, s as Sch, v, path, e);
      return e.length === 0;
    });
    if (ok.length !== 1) errs.push(`${path}: matches ${ok.length} of oneOf`);
    return;
  }
  if ('const' in schema && v !== schema.const) errs.push(`${path}: want const ${JSON.stringify(schema.const)}`);
  if (Array.isArray(schema.enum) && !schema.enum.includes(v)) errs.push(`${path}: not in enum`);
  const t = schema.type;
  if (t === 'string' && typeof v !== 'string') return void errs.push(`${path}: want string`);
  if (t === 'number' && typeof v !== 'number') return void errs.push(`${path}: want number`);
  if (t === 'integer' && !Number.isInteger(v)) return void errs.push(`${path}: want integer`);
  if (t === 'array' && !Array.isArray(v)) return void errs.push(`${path}: want array`);
  if (t === 'object' && (typeof v !== 'object' || v === null || Array.isArray(v))) return void errs.push(`${path}: want object`);
  if (typeof v === 'string' && typeof schema.pattern === 'string' && !new RegExp(schema.pattern).test(v)) {
    errs.push(`${path}: does not match ${schema.pattern}`);
  }
  if (typeof v === 'string' && typeof schema.minLength === 'number' && v.length < schema.minLength) errs.push(`${path}: too short`);
  if (typeof v === 'number') {
    if (typeof schema.minimum === 'number' && v < schema.minimum) errs.push(`${path}: below minimum`);
    if (typeof schema.maximum === 'number' && v > schema.maximum) errs.push(`${path}: above maximum`);
    if (typeof schema.exclusiveMinimum === 'number' && v <= schema.exclusiveMinimum) errs.push(`${path}: not above minimum`);
  }
  if (Array.isArray(v)) {
    if (typeof schema.minItems === 'number' && v.length < schema.minItems) errs.push(`${path}: too few items`);
    if (schema.items) v.forEach((x, i) => check(root, schema.items as Sch, x, `${path}[${i}]`, errs));
  }
  if (t === 'object' && typeof v === 'object' && v !== null) {
    const o = v as Record<string, unknown>;
    const props = (schema.properties ?? {}) as Record<string, Sch>;
    for (const r of (schema.required ?? []) as string[]) if (!(r in o)) errs.push(`${path}: missing ${r}`);
    for (const [k, x] of Object.entries(o)) {
      if (k in props) check(root, props[k], x, `${path}.${k}`, errs);
      else if (schema.additionalProperties === false) errs.push(`${path}: unexpected ${k}`);
      else if (typeof schema.additionalProperties === 'object') check(root, schema.additionalProperties as Sch, x, `${path}.${k}`, errs);
    }
  }
}

function errorsOf(v: unknown): string[] {
  const root = irSchema();
  const errs: string[] = [];
  check(root, root, v, '$', errs);
  return errs;
}

test('the committed ir.schema.json equals irSchema()', () => {
  const committed = readFileSync(join(here, 'ir.schema.json'), 'utf8');
  assert.equal(committed, `${JSON.stringify(irSchema(), null, 2)}\n`, 'regenerate: node emit.ts --schema > ir.schema.json');
});

test('every example IR validates against ir.schema.json', () => {
  for (const [name, make] of Object.entries(examples)) assert.deepEqual(errorsOf(make()), [], name);
});

test('the schema rejects a malformed IR', () => {
  const ir = JSON.parse(JSON.stringify(examples.reviewLoop()));
  ir.nodes[0].max_iters = 0;
  ir.nodes[0].nodes[0].kind = 'agnt';
  ir.extra = true;
  const errs = errorsOf(ir).join('\n');
  assert.match(errs, /unexpected extra/);
  assert.match(errs, /\$\.nodes\[0\]: matches 0 of oneOf/);
});
