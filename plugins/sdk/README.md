# Hive plugin SDK

`hive-plugin.mjs` is the SDK for a plugin's background process: one
dependency-free file for Node 18+. Copy it next to your entry point;
it is deliberately not a package, so a plugin installed from a git URL
needs no build or `npm install`.

```js
import { connect } from './hive-plugin.mjs';

const hive = await connect();
hive.on('SESSION_EVENT', (ev) => console.log(ev.session.name, ev.session.state));
```

**The documentation is [docs/plugins.md](../../docs/plugins.md)**, and
only there: the manifest, the environment, every request and event, the
`hive` object app plugins get, the limits, and
[what changed in each API version](../../docs/plugins.md#api-versions).
This file does not repeat any of it.

Examples: [`webhook`](../webhook/) (background),
[`session-notes`](../session-notes/) (app),
[`plan-review`](../plan-review/) (app, bundled with Hive).
