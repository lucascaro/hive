// SPDX-License-Identifier: GPL-3.0-only
// Copyright (C) 2026 Lucas Caro
// See LICENSE and ADDITIONAL-TERMS.md.

// The inspector's Pinned section (spec 566). Layout claims (the height
// cap, the timeline keeping room) are CSS and live in
// test/e2e/pinned.spec.ts; this covers what renders and what is called.

// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { act, cleanup, fireEvent, render } from '@testing-library/react';

vi.mock('../../src/bridge.js', () => ({
  GetActivity: vi.fn(() => Promise.resolve()),
  GetPins: vi.fn(() => Promise.resolve()),
  GetPinBody: vi.fn(() => Promise.resolve()),
  Unpin: vi.fn(() => Promise.resolve()),
  UnpinAll: vi.fn(() => Promise.resolve()),
  SetClipboardText: vi.fn(() => Promise.resolve()),
}));
vi.mock('../../src/app/dom.js', () => ({
  flashStatus: vi.fn(),
  reportFailure: () => () => {},
}));
const openChoiceDialog = vi.fn();
vi.mock('../../src/app/modals/choice-dialog.js', () => ({
  openChoiceDialog: (...a: unknown[]) => openChoiceDialog(...a),
}));

import * as bridge from '../../src/bridge.js';
import { ActivityPanel } from '../../src/components/activity/ActivityPanel.js';
import { ActivityTile } from '../../src/components/activity/ActivityTile.js';
import { activityStore, applyActivityFrame } from '../../src/store/activity.js';
import {
  applyPinBody,
  applyPinsPage,
  pinsStore,
  resetPinsOnSessionList,
  type Pin,
} from '../../src/store/pins.js';
import { resetStore } from '../../src/store/store.js';
import type { SessionInfo } from '../../src/app/state.js';

const SID = 's1';
const now = Date.now();
const PINS: Pin[] = [
  {
    id: '000000000000000000000003',
    title: 'Root cause',
    created_at: now - 120_000,
    body_len: 10,
  },
  {
    id: '000000000000000000000002',
    title: 'Deploy recipe',
    created_at: now - 3 * 86_400_000,
    body_len: 10,
  },
];

function session(over: Partial<SessionInfo> = {}): SessionInfo {
  return {
    id: SID,
    name: 'api',
    state: 'idle',
    state_source: 'hook',
    pin_count: 2,
    ...over,
  } as SessionInfo;
}

const flush = () =>
  act(async () => {
    await Promise.resolve();
    await Promise.resolve();
  });

async function renderWithPins(node = <ActivityPanel sessionId={SID} />) {
  const r = render(node);
  await flush();
  act(() => applyPinsPage({ session_id: SID, pins: PINS, done: true }));
  return r;
}

beforeEach(() => {
  resetStore({ sessions: [session()] });
  activityStore.setState({ byId: new Map() });
  pinsStore.setState({ byId: new Map(), collapsed: false });
  for (const fn of [
    bridge.GetPins,
    bridge.GetPinBody,
    bridge.Unpin,
    bridge.UnpinAll,
    bridge.SetClipboardText,
  ]) {
    vi.mocked(fn).mockClear();
  }
  openChoiceDialog.mockReset();
});
afterEach(cleanup);

describe('Pinned section', () => {
  it('is absent, and fetches nothing, for a session with no pins', async () => {
    resetStore({ sessions: [session({ pin_count: 0 })] });
    const { container } = render(<ActivityPanel sessionId={SID} />);
    await flush();
    expect(container.querySelector('.hv-activity__pins')).toBeNull();
    expect(bridge.GetPins).not.toHaveBeenCalled();
  });

  it('refetches pins it still holds after a reconnect reports zero, and drops them', async () => {
    const { container } = await renderWithPins();
    expect(container.querySelectorAll('.hv-activity__pin')).toHaveLength(2);
    // Another window removed them while this one was disconnected.
    act(() => {
      resetStore({ sessions: [session({ pin_count: 0 })] });
      resetPinsOnSessionList(new Set([SID]));
    });
    await flush();
    expect(bridge.GetPins).toHaveBeenCalledTimes(2);
    act(() => applyPinsPage({ session_id: SID, pins: [], done: true }));
    expect(container.querySelector('.hv-activity__pins')).toBeNull();
  });

  it('lists pins newest first, one line each, with the count in the header', async () => {
    const { container } = await renderWithPins();
    expect(bridge.GetPins).toHaveBeenCalledWith(SID);
    const rows = container.querySelectorAll('.hv-activity__pin');
    expect(
      [...rows].map(
        (r) => r.querySelector('.hv-activity__pin-title')?.textContent,
      ),
    ).toEqual(['Root cause', 'Deploy recipe']);
    expect(
      container.querySelector('.hv-activity__pins-head .hv-activity__pill'),
    ).toHaveTextContent('2');
    // Collapsed rows carry no body; ages read in minutes and days.
    expect(container.querySelector('.hv-activity__pin-body')).toBeNull();
    expect(rows[0].querySelector('.hv-activity__age')).toHaveTextContent('2m');
    expect(rows[1].querySelector('.hv-activity__age')).toHaveTextContent('3d');
  });

  it('renders for a session whose agent reports no activity', async () => {
    resetStore({ sessions: [session({ state_source: 'heuristic' })] });
    const { container } = await renderWithPins();
    act(() =>
      applyActivityFrame({ session_id: SID, full: true, events: [], plan: [] }),
    );
    expect(container.querySelector('.hv-activity__empty')).not.toBeNull();
    expect(container.querySelectorAll('.hv-activity__pin')).toHaveLength(2);
  });

  it('opens a pin on click, fetches its body and renders it as markdown', async () => {
    const { container } = await renderWithPins();
    fireEvent.click(
      container.querySelector('.hv-activity__pin-main') as Element,
    );
    await flush();
    expect(bridge.GetPinBody).toHaveBeenCalledWith(SID, PINS[0].id);
    expect(container.querySelector('.hv-activity__pin-body')).toHaveTextContent(
      'Loading…',
    );
    act(() =>
      applyPinBody({
        session_id: SID,
        id: PINS[0].id,
        body: 'It was **DNS**.',
      }),
    );
    const body = container.querySelector('.hv-activity__pin-body');
    expect(body?.querySelector('strong')).toHaveTextContent('DNS');
  });

  it('collapses the whole section to its header', async () => {
    const { container } = await renderWithPins();
    fireEvent.click(
      container.querySelector('.hv-activity__pins-toggle') as Element,
    );
    expect(container.querySelector('.hv-activity__pin-list')).toBeNull();
    expect(
      container.querySelector('.hv-activity__pins-head .hv-activity__pill'),
    ).toHaveTextContent('2');
  });

  it('unpins one pin', async () => {
    const { container } = await renderWithPins();
    const unpin = container
      .querySelectorAll('.hv-activity__pin')[1]
      .querySelector('[aria-label="Unpin"]');
    fireEvent.click(unpin as Element);
    expect(bridge.Unpin).toHaveBeenCalledWith(SID, PINS[1].id);
  });

  it('asks before unpinning all, and does nothing on cancel', async () => {
    const { container } = await renderWithPins();
    openChoiceDialog.mockResolvedValueOnce('cancel');
    fireEvent.click(
      container.querySelector('.hv-activity__pins-clear') as Element,
    );
    await flush();
    expect(openChoiceDialog).toHaveBeenCalledTimes(1);
    expect(bridge.UnpinAll).not.toHaveBeenCalled();

    openChoiceDialog.mockResolvedValueOnce('unpin');
    fireEvent.click(
      container.querySelector('.hv-activity__pins-clear') as Element,
    );
    await flush();
    expect(bridge.UnpinAll).toHaveBeenCalledWith(SID);
  });

  it('copies a pin body to the clipboard', async () => {
    const { container } = await renderWithPins();
    act(() =>
      applyPinBody({ session_id: SID, id: PINS[0].id, body: 'the recipe' }),
    );
    fireEvent.click(
      container.querySelector('[aria-label="Copy pinned response"]') as Element,
    );
    await flush();
    expect(bridge.SetClipboardText).toHaveBeenCalledWith('the recipe');
  });

  it('never takes focus: actions are out of the tab order', async () => {
    const { container } = await renderWithPins();
    for (const b of container.querySelectorAll('.hv-activity__pins button')) {
      expect(b.getAttribute('tabindex')).toBe('-1');
    }
  });
});

describe('Pinned titles in an activity-grid tile', () => {
  it('lists titles only, with nothing focusable', async () => {
    const { container } = await renderWithPins(
      <ActivityTile sessionId={SID} />,
    );
    const items = container.querySelectorAll('.hv-activity__tile-pin');
    expect([...items].map((i) => i.textContent)).toEqual([
      'Root cause',
      'Deploy recipe',
    ]);
    expect(
      container.querySelectorAll(
        '.activity-tile button, .activity-tile [tabindex]',
      ),
    ).toHaveLength(0);
  });
});
