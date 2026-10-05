import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { examples } from './examples.ts';
import { toMermaid } from './mermaid.ts';

const emit = join(dirname(fileURLToPath(import.meta.url)), 'emit.ts');
const run = (...args: string[]) => spawnSync(process.execPath, [emit, ...args], { encoding: 'utf8' });

test('emit prints IR and Mermaid for every example', () => {
  for (const [name, make] of Object.entries(examples)) {
    const ir = run(name, '--ir');
    assert.equal(ir.status, 0, ir.stderr);
    assert.deepEqual(JSON.parse(ir.stdout), make());
    const mm = run(name, '--mermaid');
    assert.equal(mm.status, 0, mm.stderr);
    assert.equal(mm.stdout, toMermaid(make()));
    const both = run(name);
    assert.equal(both.stdout, `${ir.stdout}${mm.stdout}`);
  }
});

test('an unknown example exits non-zero', () => {
  const r = run('nope');
  assert.equal(r.status, 2);
  assert.match(r.stderr, /usage: emit\.ts/);
});
