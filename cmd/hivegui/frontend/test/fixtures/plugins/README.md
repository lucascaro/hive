# Misbehaving UI plugins

Fixtures for spec 471's containment criterion: each one breaks the app
in a different way, and the tests assert that only the plugin pays for
it. The Playwright mock dev server serves this directory at
`/plugins/<id>/` (vite.config.js); the real-daemon suite installs them
from here.

| Plugin | Misbehaviour |
|---|---|
| `ui-throws` | Its badge, banner, panel and settings section all throw on render. |
| `ui-hang-import` | Its module never finishes loading (a top-level await that never settles). |
| `ui-hang-activate` | `activate()` never resolves. |
| `ui-hang-render` | Its settings section suspends forever. |
| `ui-flood` | Re-renders as fast as it can, writes its settings in a loop, and re-subscribes to events in a loop. |
