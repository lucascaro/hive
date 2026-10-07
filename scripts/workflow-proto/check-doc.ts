#!/usr/bin/env node
// Keeps docs/design-docs/workflow-engine.md honest against this prototype
// (spec 495). The doc carries generated blocks between markers:
//
//   <!-- wf:<example>:ir:start -->       ```json …```      <!-- wf:<example>:ir:end -->
//   <!-- wf:<example>:mermaid:start -->  ```mermaid …```   <!-- wf:<example>:mermaid:end -->
//   <!-- wf:schema:start -->             ```json …```      <!-- wf:schema:end -->
//
// It fails when a block differs from what the prototype generates, and also
// when the doc could pass without checking anything: no blocks at all, an
// example with no ir or mermaid block, the schema block missing, a marker
// naming an unknown example or kind, a start with no end, or a duplicated
// or nested block.
//
//   node scripts/workflow-proto/check-doc.ts <doc.md> [--write]

import { readFileSync, writeFileSync } from 'node:fs';
import { pathToFileURL } from 'node:url';
import { validate } from './dsl.ts';
import { examples } from './examples.ts';
import { irSchema } from './ir-schema.ts';
import { toMermaid } from './mermaid.ts';

const KINDS = ['ir', 'mermaid'] as const;

/** The generated text for a block key (`schema`, or `<example>:<kind>`). */
export function render(key: string): string {
  if (key === 'schema') return fence('json', JSON.stringify(irSchema(), null, 2));
  const [name, kind] = key.split(':');
  const ir = examples[name]();
  const errors = validate(ir);
  if (errors.length > 0) throw new Error(`example ${name} is invalid:\n  ${errors.join('\n  ')}`);
  return kind === 'ir' ? fence('json', JSON.stringify(ir, null, 2)) : fence('mermaid', toMermaid(ir).trimEnd());
}

function fence(lang: string, body: string): string {
  return `\n\`\`\`${lang}\n${body}\n\`\`\`\n`;
}

export function expectedKeys(): string[] {
  return ['schema', ...Object.keys(examples).flatMap((n) => KINDS.map((k) => `${n}:${k}`))];
}

interface Block {
  key: string;
  start: number; // index just after the start marker
  end: number; // index of the end marker
}

const MARKER = /<!-- wf:([^\s]+?):(start|end) -->/g;

/** Parses the marker blocks, collecting structural errors. */
export function parseBlocks(md: string): { blocks: Block[]; errors: string[] } {
  const blocks: Block[] = [];
  const errors: string[] = [];
  const known = new Set(expectedKeys());
  let open: { key: string; start: number } | null = null;
  for (const m of md.matchAll(MARKER)) {
    const [text, key, edge] = m;
    const at = m.index ?? 0;
    if (!known.has(key)) {
      errors.push(`unknown block "${key}" (known: ${[...known].join(', ')})`);
      continue;
    }
    if (edge === 'start') {
      if (open) errors.push(`block "${key}" starts inside block "${open.key}" (nested)`);
      open = { key, start: at + text.length };
    } else if (!open || open.key !== key) {
      errors.push(`block "${key}" ends without a matching start`);
    } else {
      if (blocks.some((b) => b.key === key)) errors.push(`block "${key}" appears more than once`);
      blocks.push({ key, start: open.start, end: at });
      open = null;
    }
  }
  if (open) errors.push(`block "${open.key}" has no end marker`);
  return { blocks, errors };
}

/** Every error that would fail CI; empty means the doc is in step. */
export function checkDoc(md: string): string[] {
  const { blocks, errors } = parseBlocks(md);
  if (blocks.length === 0) errors.push('the doc has no <!-- wf:…:start/end --> blocks');
  const have = new Set(blocks.map((b) => b.key));
  for (const key of expectedKeys()) if (!have.has(key)) errors.push(`block "${key}" is missing`);
  for (const b of blocks) {
    if (md.slice(b.start, b.end) !== render(b.key)) {
      errors.push(`block "${b.key}" differs from the prototype; run check-doc.ts --write`);
    }
  }
  return errors;
}

/** Rewrites every existing block with its generated text. */
export function writeDoc(md: string): string {
  const { blocks, errors } = parseBlocks(md);
  if (errors.length > 0) throw new Error(errors.join('\n'));
  let out = md;
  for (const b of [...blocks].reverse()) out = out.slice(0, b.start) + render(b.key) + out.slice(b.end);
  return out;
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  const [doc, flag] = process.argv.slice(2);
  if (!doc) {
    process.stderr.write('usage: check-doc.ts <doc.md> [--write]\n');
    process.exitCode = 2;
  } else if (flag === '--write') {
    writeFileSync(doc, writeDoc(readFileSync(doc, 'utf8')));
  } else {
    const errors = checkDoc(readFileSync(doc, 'utf8'));
    for (const e of errors) process.stderr.write(`check-doc: ${e}\n`);
    process.exitCode = errors.length > 0 ? 1 : 0;
  }
}
