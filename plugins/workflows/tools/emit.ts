#!/usr/bin/env node
// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// Prints a worked example's IR JSON and/or Mermaid (spec 495).
//
//   node plugins/workflows/tools/emit.ts <example> [--ir | --mermaid]
//   node plugins/workflows/tools/emit.ts --schema > plugins/workflows/ir/ir.schema.json
//
// With no flag it prints both, IR first. Exits 2 on an unknown example and 1
// when the example fails validation.

import { validate } from '../ir/validate.ts';
import { examples } from './examples.ts';
import { irSchema } from '../ir/ir-schema.ts';
import { toMermaid } from '../ir/mermaid.ts';

const [name, flag] = process.argv.slice(2);
if (name === '--schema') {
  process.stdout.write(`${JSON.stringify(irSchema(), null, 2)}\n`);
} else if (!name || !Object.hasOwn(examples, name)) {
  process.stderr.write(`usage: emit.ts <${Object.keys(examples).join('|')}> [--ir|--mermaid]\n`);
  process.exitCode = 2;
} else {
  const ir = examples[name]();
  const errors = validate(ir);
  if (errors.length > 0) {
    for (const e of errors) process.stderr.write(`emit: ${e}\n`);
    process.exitCode = 1;
  } else {
    if (flag !== '--mermaid') process.stdout.write(`${JSON.stringify(ir, null, 2)}\n`);
    if (flag !== '--ir') process.stdout.write(toMermaid(ir));
  }
}
