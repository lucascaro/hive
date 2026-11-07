# Workflows plugin (headless engine, in progress)

This folder backs the design in
[docs/design-docs/workflow-engine.md](../../docs/design-docs/workflow-engine.md)
(specs 495 and 529). It holds the typed workflow builder, the IR and its
validator today; the interpreter and run log arrive in later phases of
spec 529. It is not embedded in `hived` yet.

- `sdk/`: the builder (`dsl.ts`, `schema.ts`) and the generic constructs
  `reviewLoop`, `fanOutThenVerify` and `planImplementVerify` (`library.ts`).
- `ir/`: the IR's JSON Schema (`ir-schema.ts`, committed as
  `ir.schema.json`), the Mermaid renderer, and the three validation passes:
  `shape.ts` (structure of unknown JSON), `validate.ts` (semantics) and
  `catalog.ts` (agents and modes against the daemon's `ACP_AGENTS`).
  `resultschema.ts` is the TypeScript twin of `internal/acp/resultschema.go`.
- `tools/`: `emit.ts`, `check-doc.ts` and the worked `examples.ts`.
- `test/`: the tests and the type-level checks (`types.check.ts`).

## Run it

```bash
node plugins/workflows/tools/emit.ts reviewLoop            # IR JSON, then Mermaid
node plugins/workflows/tools/emit.ts fanOutThenVerify --mermaid
node plugins/workflows/tools/emit.ts --schema >| plugins/workflows/ir/ir.schema.json

node --test 'plugins/workflows/**/*.test.ts'
node plugins/workflows/tools/check-doc.ts docs/design-docs/workflow-engine.md          # CI gate
node plugins/workflows/tools/check-doc.ts docs/design-docs/workflow-engine.md --write  # regenerate blocks

cmd/hivegui/frontend/node_modules/.bin/tsc -p plugins/workflows   # type-check, incl. types.check.ts
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
