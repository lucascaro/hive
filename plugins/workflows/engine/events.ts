// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The run event schema (spec 529, Contract for C) and what happens to an event
// before it is stored: redaction first (so a secret cut in half by truncation
// is not missed), then the write-time size cap (B-11). The stored event is the
// event every client sees, live or replayed.

import { redactValue } from './redact.ts';
import { encodedSize, MAX_MESSAGE_BYTES, truncateEncoded } from './size.ts';

export type EventType =
  | 'run.started'
  | 'run.finished'
  | 'run.retried'
  | 'node.reset'
  | 'node.ready'
  | 'node.started'
  | 'node.waiting'
  | 'node.resumed'
  | 'node.output'
  | 'node.finished'
  | 'loop.iteration'
  | 'map.expanded'
  | 'agent.turn.started'
  | 'agent.turn.finished'
  | 'agent.message'
  | 'agent.tool'
  | 'agent.plan'
  | 'agent.usage'
  | 'check.output';

export interface EventDraft {
  key: string | null;
  node_id: string | null;
  attempt: number;
  type: EventType;
  data: Record<string, unknown>;
}

export interface RunEvent extends EventDraft {
  run_id: string;
  /** Per run, gapless, from 1. */
  seq: number;
  /** RFC 3339. */
  ts: string;
  data: Record<string, unknown> & { redacted?: number };
}

/** Every stored event is at most this big once Go has re-encoded it (B-11): a history page always has room for one. */
export const MAX_EVENT_BYTES = 24 << 10;
/** A `node.output` event carries at most this much of the output; the rest is paged through node.get (B-7). */
export const MAX_OUTPUT_EVENT_BYTES = 16 << 10;
const MAX_TEXT_BYTES = 16 << 10;

/** Permission ids the GUI sends back to the daemon verbatim (B-16). */
const SAFE_ID = /^[A-Za-z0-9._:-]{1,256}$/;

/** Stands in for a permission id that fails SAFE_ID; the real id stays in the store (B-16). */
export interface Handles {
  handleFor(realId: string): string;
}

type Data = Record<string, unknown>;

/**
 * A `node.waiting` for a permission carries ids that must reach
 * ANSWER_PERMISSION untouched, so a token-shaped id must not be redacted
 * (B-16). Exactly `request_id` and each `option_id` are exempt, and only when
 * they pass SAFE_ID; any other value is swapped for an opaque handle first.
 * Everything else is redacted as usual.
 */
function redactWaiting(data: Data, handles: Handles): { value: Data; count: number } {
  const id = (v: unknown): string => (typeof v === 'string' && SAFE_ID.test(v) ? v : handles.handleFor(String(v)));
  const options = Array.isArray(data.options) ? (data.options as unknown[]).filter((o): o is Data => typeof o === 'object' && o !== null && !Array.isArray(o)) : [];
  const { request_id, options: _o, ...rest } = data;
  const r = redactValue(rest);
  const redactedOptions = options.map((o) => {
    const { option_id, ...others } = o;
    const ro = redactValue(others);
    r.count += ro.count;
    return { option_id: id(option_id), ...ro.value };
  });
  return { value: { request_id: id(request_id), ...r.value, options: redactedOptions }, count: r.count };
}

/** Redacts an event's data, counting replacements in `data.redacted`, then caps its size. */
export function prepareEvent(draft: EventDraft, handles: Handles): EventDraft {
  const waitingPerm = draft.type === 'node.waiting' && draft.data.reason === 'permission';
  const r = waitingPerm ? redactWaiting(draft.data, handles) : redactValue(draft.data);
  const data: Data = r.count > 0 ? { ...r.value, redacted: r.count } : r.value;
  return { ...draft, data: capData(draft.type, data) };
}

/** Cuts a text field to a budget and notes that it was cut. */
function cutText(data: Data, field: string, max: number): void {
  const v = data[field];
  if (typeof v !== 'string') return;
  const r = truncateEncoded(v, max);
  if (r.truncated) {
    data[field] = r.text;
    data.truncated = true;
  }
}

export function capData(type: EventType, input: Data): Data {
  const data: Data = { ...input };
  switch (type) {
    case 'node.output':
      if (encodedSize(data.output) > MAX_OUTPUT_EVENT_BYTES) {
        // The output as one string: the first part of its serialized form. node.get serves the rest.
        const s = JSON.stringify(data.output) ?? '';
        data.output = truncateEncoded(s, MAX_OUTPUT_EVENT_BYTES - 64).text;
        data.truncated = true;
      }
      break;
    case 'agent.message':
    case 'check.output':
      cutText(data, 'text', MAX_TEXT_BYTES);
      break;
    case 'agent.tool':
      cutText(data, 'title', 1 << 10);
      cutText(data, 'text', MAX_TEXT_BYTES);
      break;
    case 'agent.plan': {
      const entries = Array.isArray(data.entries) ? [...(data.entries as unknown[])] : [];
      let omitted = 0;
      while (entries.length > 0 && encodedSize({ ...data, entries, entries_omitted: omitted }) > MAX_EVENT_BYTES) {
        entries.pop();
        omitted++;
      }
      data.entries = entries;
      if (omitted > 0) data.entries_omitted = omitted;
      break;
    }
    default:
      break;
  }
  // Whatever is still over (an `error`, a `kept_worktrees` list, an odd field): halve the longest string until it fits.
  for (let guard = 0; guard < 64 && encodedSize(data) > MAX_EVENT_BYTES; guard++) {
    const longest = Object.entries(data)
      .filter(([, v]) => typeof v === 'string')
      .sort((a, b) => (b[1] as string).length - (a[1] as string).length)[0];
    if (!longest) {
      return { truncated: true };
    }
    data[longest[0]] = (longest[1] as string).slice(0, Math.max(0, Math.floor((longest[1] as string).length / 2)));
    data.truncated = true;
  }
  return data;
}

/** Splits events into `run.event` pushes that each fit the 48 KiB message budget, keeping order. */
export function batchForPush(runId: string, events: readonly RunEvent[]): RunEvent[][] {
  const overhead = encodedSize({ plugin_id: 'workflows', topic: 'run.event', data: { run_id: runId, events: [] } });
  const batches: RunEvent[][] = [];
  let cur: RunEvent[] = [];
  let size = overhead;
  for (const ev of events) {
    const n = encodedSize(ev) + 1;
    if (cur.length > 0 && size + n > MAX_MESSAGE_BYTES) {
      batches.push(cur);
      cur = [];
      size = overhead;
    }
    cur.push(ev);
    size += n;
  }
  if (cur.length > 0) batches.push(cur);
  return batches;
}
