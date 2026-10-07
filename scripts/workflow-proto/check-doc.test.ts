import assert from 'node:assert/strict';
import { spawnSync } from 'node:child_process';
import { mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { test } from 'node:test';
import { fileURLToPath } from 'node:url';
import { checkDoc, expectedKeys, render, writeDoc } from './check-doc.ts';

const doc = join(dirname(fileURLToPath(import.meta.url)), '../../docs/design-docs/workflow-engine.md');

const block = (key: string, body = render(key)) => `<!-- wf:${key}:start -->${body}<!-- wf:${key}:end -->`;
// A minimal doc that is fully in step: every expected block, in order.
const good = () => `# Doc\n\n${expectedKeys().map((k) => block(k)).join('\n\n')}\n`;

test('passes on the committed doc', () => {
  assert.deepEqual(checkDoc(readFileSync(doc, 'utf8')), []);
});

test('passes on a minimal in-step doc', () => {
  assert.deepEqual(checkDoc(good()), []);
});

test('fails when an IR block is edited', () => {
  const md = good().replace('"kind": "agent"', '"kind": "agnt"');
  assert.notEqual(md, good());
  assert.match(checkDoc(md).join('\n'), /block "reviewLoop:ir" differs from the prototype/);
});

test('fails when the doc has no wf blocks', () => {
  const errors = checkDoc('# Doc\n\nNo blocks here.\n').join('\n');
  assert.match(errors, /the doc has no <!-- wf:…:start\/end --> blocks/);
  assert.match(errors, /block "schema" is missing/);
});

test('fails when an example has no ir block', () => {
  const md = good().replace(block('fanOutThenVerify:ir'), '');
  assert.deepEqual(checkDoc(md), ['block "fanOutThenVerify:ir" is missing']);
});

test('fails when an example has no mermaid block', () => {
  const md = good().replace(block('planImplementVerify:mermaid'), '');
  assert.deepEqual(checkDoc(md), ['block "planImplementVerify:mermaid" is missing']);
});

test('fails when the wf:schema block is missing', () => {
  const md = good().replace(block('schema'), '');
  assert.deepEqual(checkDoc(md), ['block "schema" is missing']);
});

test('fails when the schema block drifts from irSchema()', () => {
  const md = good().replace('"title": "Hive workflow IR"', '"title": "Old"');
  assert.match(checkDoc(md).join('\n'), /block "schema" differs/);
});

test('fails on an unknown example name or kind', () => {
  const errors = checkDoc(`${good()}\n${block('reviewLoop:ir').replaceAll('reviewLoop:ir', 'nope:ir')}\n<!-- wf:reviewLoop:svg:start --><!-- wf:reviewLoop:svg:end -->`);
  assert.match(errors.join('\n'), /unknown block "nope:ir"/);
  assert.match(errors.join('\n'), /unknown block "reviewLoop:svg"/);
});

test('fails on a start without an end, and on duplicate blocks', () => {
  const unclosed = `${good()}\n<!-- wf:schema:start -->\n`;
  assert.match(checkDoc(unclosed).join('\n'), /block "schema" has no end marker/);
  const dup = `${good()}\n${block('schema')}\n`;
  assert.match(checkDoc(dup).join('\n'), /block "schema" appears more than once/);
  const stray = `${good()}\n<!-- wf:schema:end -->\n`;
  assert.match(checkDoc(stray).join('\n'), /block "schema" ends without a matching start/);
});

test('fails on nested blocks', () => {
  const md = good().replace(block('schema'), `<!-- wf:schema:start -->${block('reviewLoop:ir')}<!-- wf:schema:end -->`);
  assert.match(checkDoc(md).join('\n'), /block "reviewLoop:ir" starts inside block "schema" \(nested\)/);
});

test('--write regenerates blocks', () => {
  const stale = expectedKeys()
    .map((k) => block(k, '\nstale\n'))
    .join('\n');
  assert.ok(checkDoc(stale).length > 0);
  const fixed = writeDoc(stale);
  assert.deepEqual(checkDoc(fixed), []);
  assert.equal(writeDoc(fixed), fixed, 'idempotent');
});

test('the CLI exits 0 in step, 1 on drift and 2 without a doc', () => {
  const cli = join(dirname(fileURLToPath(import.meta.url)), 'check-doc.ts');
  const run = (...args: string[]) => spawnSync(process.execPath, [cli, ...args], { encoding: 'utf8' });
  const dir = mkdtempSync(join(tmpdir(), 'check-doc-'));
  try {
    const md = join(dir, 'doc.md');
    writeFileSync(md, good());
    assert.equal(run(md).status, 0);
    writeFileSync(md, good().replace('"kind": "agent"', '"kind": "agnt"'));
    const drift = run(md);
    assert.equal(drift.status, 1);
    assert.match(drift.stderr, /check-doc: block "reviewLoop:ir" differs/);
    assert.equal(run().status, 2);
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
});
