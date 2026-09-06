# Verifying the GUI by hand, without a human

Plans in `docs/exec-plans/` carry manual smoke checklists — rows like "run
this, watch the glyph". They exist because the automated layers cannot see
some things: the `e2e` layer runs against the Wails *mock*, and `go` tests
stop at the daemon boundary.

Those rows have a habit of never being run. They do not need eyes:
**`wails dev` publishes the real frontend, driven by the real daemon, at a
URL**, and a Playwright script can read that page's DOM directly. That is
better evidence than a screenshot — an assertion on
`svg.hv-state-icon[data-state]` cannot be misread the way a picture of a
14px glyph can.

Reach for this when a checklist row asks you to look at the running app.
For a regression you want to keep, write an `e2e-real` test instead
(`npm run test:e2e:real`, see AGENTS.md) — this recipe is for one-off
verification, and it leaves nothing behind.

## The recipe

**1. Start `wails dev` against an isolated daemon.** Never the real socket
or state dir — the same isolation rule as every other layer:

```bash
ISO=/tmp/hive-iso-$(basename "$PWD")
mkdir -p "$ISO/state"
cd cmd/hivegui && HIVE_SOCKET="$ISO/hived.sock" HIVE_STATE_DIR="$ISO/state" \
  HIVE_DEBUG_STATE=1 wails dev
```

It takes about a minute to compile, then prints:

```
To develop in the browser and call your bound Go methods from Javascript,
navigate to: http://localhost:34115
```

`:34115` is the one that matters — `:5173` is the bare Vite server, whose
Wails bindings are not connected to the Go side.

`HIVE_DEBUG_STATE=1` makes the daemon log every state transition with the
observation that caused it (`registry.go`'s `logStateLocked`). Read those
lines from the `wails dev` output.

**2. Drive it with a throwaway Playwright script.** The script must live
inside `cmd/hivegui/frontend/` — that is where `@playwright/test` resolves
from. Delete it when you are done; it is not a test, it is a probe.

```js
import { chromium } from '@playwright/test';
const b = await chromium.launch();
const p = await b.newPage({ viewport: { width: 1400, height: 900 } });
await p.goto('http://localhost:34115', { waitUntil: 'domcontentloaded' });
await p.waitForTimeout(6000); // boot: control connect + attach

const states = () => p.evaluate(() =>
  [...document.querySelectorAll('svg.hv-state-icon')]
    .map(e => ({ state: e.dataset.state, tip: e.querySelector('title')?.textContent })));

// Type into a session: click the terminal first so xterm takes focus.
await p.locator('.xterm-screen, .xterm').first().click();
await p.keyboard.type('ls -R /usr/lib | head -400\n');
await p.waitForTimeout(700);
console.log('working:', JSON.stringify(await states()));

await p.screenshot({ path: '/tmp/shot.png' });
await b.close();
```

Run it with `node probe.mjs` from `cmd/hivegui/frontend/`.

**3. Feed the agent tiers by hand.** Anything the hook tier reports can be
driven without launching a real agent, through the real `hived hook`
binary, the real event socket, and the fixtures the Go tests already use:

```bash
go build -o /tmp/hived-probe ./cmd/hived
export HIVE_SOCKET=$ISO/hived.sock HIVE_SESSION_ID=<session id>
/tmp/hived-probe hook < cmd/hived/testdata/hooks/user_prompt_submit.json
/tmp/hived-probe hook < cmd/hived/testdata/hooks/stop.json
```

Session ids are the directory names under `$ISO/state/sessions/`.

For the extension tier and for anything only a real agent produces, prefer
the opt-in probes: `HIVE_PROBE_CLAUDE=1` / `HIVE_PROBE_PI=1 go test
./cmd/hived/ -run TestClaudeProbe|TestPiProbe -v`.

## Gotchas that cost time

- **The state tooltip is an SVG `<title>` child, not a `title=` attribute.**
  Querying `[title]` finds the buttons and misses every glyph.
- **`screencapture` needs screen-recording permission** the agent process
  usually does not have, and the Chrome extension may not be connected.
  Playwright's own `page.screenshot()` needs neither.
- **A login shell decides which `node`/`python`/agent binary an agent
  session gets.** Hive spawns agents through `$SHELL -l -i -c`, so an agent
  session whose `SHELL` differs from the user's gets a different toolchain —
  and a `pi` on an old node dies in its own bundle before Hive sees it.
  Check `$SHELL` before blaming the app.
- **Never `pkill -f hivegui` or `pkill -f hived`.** Those patterns match the
  user's production Hive and every agent session inside it. Kill the iso
  daemon by the pid in `$ISO/hived.sock.pid`, and `scripts/dev-iso.sh
  --reset` for a clean slate.
- **Restarting the daemon under a running GUI is a valid test** (state must
  come back idle/heuristic/empty) — the GUI reconnects on its own.

## What this cannot reach

- **Desktop notifications** — fired by the GUI process (`cmd/hivegui/app.go`),
  which a headless browser does not have.
- **`hivebar`** — an `NSStatusItem`; its menu cannot be opened or read
  without real screen control. Still a human row.
- **Native window chrome** — window size, traffic lights, dock behaviour.
