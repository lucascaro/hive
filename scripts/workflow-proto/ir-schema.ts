// JSON Schema of the workflow IR (spec 495). `ir.schema.json` is this value,
// committed; schema.test.ts fails when the two differ, and check-doc keeps
// the copy in docs/design-docs/workflow-engine.md in step.

import { MAX_CONCURRENCY, MAX_ITERS } from './dsl.ts';

const ref = (name: string) => ({ $ref: `#/$defs/${name}` });
const obj = (properties: Record<string, unknown>, required: string[]) => ({
  type: 'object',
  properties,
  required,
  additionalProperties: false,
});

const id = { type: 'string', pattern: '^[a-z][a-z0-9]*(-[a-z0-9]+)*(/[a-z][a-z0-9]*(-[a-z0-9]+)*)*$' };
const body = { nodes: { type: 'array', items: ref('node') }, edges: { type: 'array', items: ref('edge') } };

export function irSchema(): Record<string, unknown> {
  return {
    $schema: 'https://json-schema.org/draft/2020-12/schema',
    $id: 'https://github.com/lucascaro/hive/scripts/workflow-proto/ir.schema.json',
    title: 'Hive workflow IR',
    ...obj({ ir: { const: 1 }, name: { type: 'string' }, input: ref('objectSchema'), ...body }, [
      'ir',
      'name',
      'input',
      'nodes',
      'edges',
    ]),
    $defs: {
      node: {
        oneOf: [ref('agent'), ref('check'), ref('human'), ref('group'), ref('loop'), ref('map')],
      },
      agent: obj(
        {
          id,
          kind: { const: 'agent' },
          agent: { type: 'string', minLength: 1 },
          mode: { type: 'string' },
          worktree: { oneOf: [{ const: 'own' }, obj({ of: id }, ['of'])] },
          timeout_s: { type: 'number', exclusiveMinimum: 0 },
          prompt: { type: 'string' },
          output: ref('objectSchema'),
        },
        ['id', 'kind', 'agent', 'worktree', 'timeout_s', 'prompt', 'output'],
      ),
      check: obj(
        {
          id,
          kind: { const: 'check' },
          argv: { type: 'array', items: { type: 'string' }, minItems: 1 },
          timeout_s: { type: 'number', exclusiveMinimum: 0 },
        },
        ['id', 'kind', 'argv', 'timeout_s'],
      ),
      human: obj({ id, kind: { const: 'human' }, prompt: { type: 'string' }, output: ref('objectSchema') }, [
        'id',
        'kind',
        'prompt',
        'output',
      ]),
      group: obj({ id, kind: { const: 'group' }, ...body }, ['id', 'kind', 'nodes', 'edges']),
      loop: obj(
        {
          id,
          kind: { const: 'loop' },
          max_iters: { type: 'integer', minimum: 1, maximum: MAX_ITERS },
          until: ref('cond'),
          ...body,
        },
        ['id', 'kind', 'max_iters', 'until', 'nodes', 'edges'],
      ),
      map: obj(
        {
          id,
          kind: { const: 'map' },
          over: { type: 'string' },
          concurrency: { type: 'integer', minimum: 1, maximum: MAX_CONCURRENCY },
          ...body,
        },
        ['id', 'kind', 'over', 'concurrency', 'nodes', 'edges'],
      ),
      edge: obj({ from: id, to: id, when: ref('cond') }, ['from', 'to']),
      cond: {
        oneOf: [
          obj({ ref: { type: 'string' }, op: { enum: ['eq', 'ne', 'in', 'truthy', 'lt', 'gt'] }, value: {} }, ['ref', 'op']),
          obj({ all: { type: 'array', items: ref('cond') } }, ['all']),
          obj({ any: { type: 'array', items: ref('cond') } }, ['any']),
          obj({ not: ref('cond') }, ['not']),
        ],
      },
      objectSchema: obj(
        {
          type: { const: 'object' },
          properties: { type: 'object', additionalProperties: ref('valueSchema') },
          required: { type: 'array', items: { type: 'string' } },
          additionalProperties: { const: false },
        },
        ['type', 'properties', 'required', 'additionalProperties'],
      ),
      valueSchema: {
        oneOf: [
          obj({ type: { const: 'string' }, enum: { type: 'array', items: { type: 'string' }, minItems: 1 } }, ['type']),
          obj({ type: { const: 'number' } }, ['type']),
          obj({ type: { const: 'boolean' } }, ['type']),
          obj({ type: { const: 'array' }, items: ref('valueSchema') }, ['type', 'items']),
          ref('objectSchema'),
        ],
      },
    },
  };
}
