// Webhook — the reference Hive plugin. POSTs a JSON message to a URL
// when a session starts waiting for you: its agent is waiting for input,
// or its terminal rang the bell. See README.md.

import { readFileSync } from 'node:fs';
import { join } from 'node:path';
import { connect } from './hive-plugin.mjs';

function loadConfig() {
  const file = join(process.env.HIVE_PLUGIN_DATA_DIR ?? '.', 'config.json');
  try {
    const cfg = JSON.parse(readFileSync(file, 'utf8'));
    if (typeof cfg.url !== 'string' || !/^https?:\/\//.test(cfg.url)) {
      throw new Error('"url" must be an http(s) URL');
    }
    return cfg;
  } catch (err) {
    console.error(`webhook: no usable config at ${file}: ${err.message}`);
    return null;
  }
}

const config = loadConfig();
const hive = await connect();

/** Last seen {state, needs_attention} per session, so each fires once. */
const seen = new Map();

function remember(s) {
  seen.set(s.id, { state: s.state ?? '', attention: Boolean(s.needs_attention) });
}

async function post(reason, s) {
  if (!config) return;
  const body = {
    event: reason,
    session_id: s.id,
    name: s.name,
    project_id: s.project_id ?? '',
    state: s.state ?? '',
    needs_attention: Boolean(s.needs_attention),
    title: s.title ?? '',
    at: new Date().toISOString(),
  };
  try {
    const res = await fetch(config.url, {
      method: 'POST',
      headers: { 'content-type': 'application/json' },
      body: JSON.stringify(body),
      signal: AbortSignal.timeout(10_000),
    });
    if (!res.ok) console.error(`webhook: ${config.url} answered ${res.status}`);
  } catch (err) {
    console.error(`webhook: POST ${config.url} failed: ${err.message}`);
  }
}

// The snapshot after connecting seeds what is already waiting, so a
// restart does not re-announce it.
hive.on('SESSIONS', (resp) => {
  for (const s of resp.sessions ?? []) remember(s);
});

hive.on('SESSION_EVENT', (ev) => {
  const s = ev.session;
  if (ev.kind === 'removed') {
    seen.delete(s.id);
    return;
  }
  const before = seen.get(s.id) ?? { state: '', attention: false };
  remember(s);
  if (s.state === 'waiting_input' && before.state !== 'waiting_input') {
    post('waiting_input', s);
  } else if (s.needs_attention && !before.attention) {
    post('attention', s);
  }
});

console.log(`webhook: connected; posting to ${config?.url ?? '(not configured)'}`);
