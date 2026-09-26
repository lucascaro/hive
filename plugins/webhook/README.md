# Webhook

The reference Hive plugin. It POSTs a small JSON message to a URL of
your choice whenever a session starts waiting for you: its agent is
waiting for input, or its terminal rang the bell. Point it at a Slack
or Discord incoming webhook, ntfy, Home Assistant, or your own server.

It is written only against [docs/plugins.md](../../docs/plugins.md) and
the SDK file next to it, so it doubles as a worked example.

## Install

Needs `node` (18 or newer) on the PATH Hive runs with.

Install from this directory, or from a git URL of a copy of it. Hive
installs it disabled and asks before enabling it.

## Configure

Create `config.json` in the plugin's data directory, which Hive passes
to the plugin as `HIVE_PLUGIN_DATA_DIR` — `plugin-data/webhook/` under
Hive's state directory:

```json
{ "url": "https://example.com/hive-hook" }
```

The data directory survives removing and reinstalling the plugin.
Restart the plugin (disable, then enable) after editing it.

## What it sends

```json
{
  "event": "waiting_input",
  "session_id": "…",
  "name": "fix-login",
  "project_id": "…",
  "state": "waiting_input",
  "needs_attention": false,
  "title": "…",
  "at": "2026-09-25T20:00:00.000Z"
}
```

`event` is `waiting_input` when a session's agent starts waiting for
input, or `attention` when its terminal rings the bell. Each fires once
per transition; sessions that were already waiting when the plugin
started are not announced.

Delivery failures are written to `plugin.log` in the data directory and
not retried.
