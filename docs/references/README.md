# References

External documentation pulled into the repo so agents can read it in-context. Anything an agent should be able to consult without leaving the repo goes here.

Common contents:

- `*-llms.txt` — vendor-provided LLM-friendly docs (e.g. `nixpacks-llms.txt`, `uv-llms.txt`).
- Article summaries, design system references, API specs, anything stable enough to commit.

## Conventions

- One file per source. Filename describes what it is, not where it came from.
- Include the source URL and fetch date at the top of each file.
- Refresh on a schedule that matches how fast the upstream changes.
