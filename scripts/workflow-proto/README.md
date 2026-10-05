# Workflow prototype

This folder backs the design in
[docs/design-docs/workflow-engine.md](../../docs/design-docs/workflow-engine.md)
(spec 495). It contains:
- a typed TypeScript workflow builder (`dsl.ts`, `schema.ts`);
- the generic constructs `reviewLoop`, `fanOutThenVerify` and
  `planImplementVerify` (`library.ts`);
- the IR's JSON Schema (`ir-schema.ts`, committed as `ir.schema.json`);
- a Mermaid renderer (`mermaid.ts`).

It **executes no agents**, has no interpreter and no run log. It builds graphs
and checks them.

**No dependencies.** It needs Node 24, or Node 22.18 or later, which runs `.ts`
files natively by stripping the types. The `package.json` here only marks the
folder as ES modules; there is nothing to install.

## Run it

```bash
node scripts/workflow-proto/emit.ts reviewLoop            # IR JSON, then Mermaid
node scripts/workflow-proto/emit.ts fanOutThenVerify --mermaid
node scripts/workflow-proto/emit.ts --schema >| scripts/workflow-proto/ir.schema.json

node --test 'scripts/workflow-proto/*.test.ts'
node scripts/workflow-proto/check-doc.ts docs/design-docs/workflow-engine.md          # CI gate
node scripts/workflow-proto/check-doc.ts docs/design-docs/workflow-engine.md --write  # regenerate blocks

cmd/hivegui/frontend/node_modules/.bin/tsc -p scripts/workflow-proto   # type-check, incl. types.check.ts
```

`check-doc.ts` regenerates every `<!-- wf:…:start/end -->` block in the design
doc and fails on any difference. It also fails when a block is missing, when an
example or kind is unknown, or when markers are unmatched, duplicated or
nested, so the doc cannot drift from the code or quietly stop being checked.

`types.check.ts` is compiled but never run. Each `@ts-expect-error` line in it
is a mistake the builder's types must reject; `tsc` fails if one stops being
an error.

## Not linted

CI runs Biome only from `cmd/hivegui/frontend`. These files are therefore
formatted by hand, the same as `scripts/acp-probe/`. The type-check and tests
run in CI on the `matrix.biome` leg (`.github/workflows/ci.yml`).
