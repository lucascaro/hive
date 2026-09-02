// @vitest-environment jsdom
//
// Banner.tsx in isolation — the presentational half of a banner, ported
// from src/ui/banner.ts. Replaces the deleted test/dom/ui-banner.test.ts:
// same cases, now asserting on the rendered DOM via <Banner /> instead
// of a returned handle object.
import { describe, it, expect, vi } from 'vitest';
import { render, cleanup } from '@testing-library/react';
import { Banner } from '../../src/components/Banner.js';
import type { BannerData } from '../../src/store/store.js';

function mkData(patch: Partial<BannerData> = {}): BannerData {
  return { text: '', visible: false, ...patch };
}

describe('Banner', () => {
  it('starts hidden, with the kind as data and the right aria role', () => {
    render(
      <Banner
        slot="daemon"
        kind="error"
        data={mkData({ text: 'daemon build mismatch' })}
      />,
    );
    const el = document.querySelector('.hv-banner') as HTMLElement;
    expect(el.hidden).toBe(true);
    expect(el.dataset.kind).toBe('error');
    expect(el.getAttribute('role')).toBe('alert');
    expect(el.querySelector('.hv-banner__text')?.textContent).toBe(
      'daemon build mismatch',
    );
  });

  it('uses role=status for the info kind', () => {
    render(<Banner slot="update" kind="info" data={mkData()} />);
    expect(document.querySelector('.hv-banner')?.getAttribute('role')).toBe(
      'status',
    );
  });

  it('exposes actions by data-action-id and runs their handler', () => {
    const spy = vi.fn();
    render(
      <Banner
        slot="daemon"
        kind="error"
        data={mkData()}
        actions={[{ id: 'restart', label: 'Restart Hive', onClick: spy }]}
      />,
    );
    const btn = document.querySelector<HTMLButtonElement>(
      '[data-action-id="restart"]',
    );
    expect(btn?.textContent).toBe('Restart Hive');
    btn?.click();
    expect(spy).toHaveBeenCalledTimes(1);
  });

  it('renders a dismiss icon button only when onDismiss is given', () => {
    const spy = vi.fn();
    render(
      <Banner slot="update" kind="info" data={mkData()} onDismiss={spy} />,
    );
    const dismiss = document.querySelector<HTMLButtonElement>(
      '.hv-banner__dismiss',
    );
    expect(dismiss?.getAttribute('aria-label')).toBe('Dismiss');
    dismiss?.click();
    expect(spy).toHaveBeenCalledTimes(1);

    cleanup();
    render(<Banner slot="update" kind="info" data={mkData()} />);
    expect(document.querySelector('.hv-banner__dismiss')).toBeNull();
  });

  it('applies per-action label/disabled overrides from BannerData.actions', () => {
    render(
      <Banner
        slot="update"
        kind="info"
        data={mkData({
          actions: { action: { label: 'Restart', disabled: true } },
        })}
        actions={[{ id: 'action', label: 'Update', onClick: () => {} }]}
      />,
    );
    const btn = document.querySelector<HTMLButtonElement>(
      '[data-action-id="action"]',
    );
    expect(btn?.textContent).toBe('Restart');
    expect(btn?.disabled).toBe(true);
  });

  it('hides an action via BannerData.actions', () => {
    render(
      <Banner
        slot="update"
        kind="info"
        data={mkData({ actions: { download: { hidden: true } } })}
        actions={[{ id: 'download', label: 'Download', onClick: () => {} }]}
      />,
    );
    expect(
      document.querySelector<HTMLButtonElement>('[data-action-id="download"]')
        ?.hidden,
    ).toBe(true);
  });

  it('stamps root data-* from BannerData.data, kebab-casing camelCase keys', () => {
    // The store keeps the dataset spelling the old imperative code used
    // (el.dataset.daemonBuild); Banner.tsx converts it to the kebab-case
    // attribute React needs, once, at the root.
    render(
      <Banner
        slot="daemon"
        kind="error"
        data={mkData({ data: { daemonBuild: 'abc123' } })}
      />,
    );
    expect(
      document.querySelector('.hv-banner')?.getAttribute('data-daemon-build'),
    ).toBe('abc123');
  });

  it('drives text and visibility off BannerData on rerender', () => {
    const { rerender } = render(
      <Banner slot="update" kind="info" id="update-banner" data={mkData()} />,
    );
    const el = document.getElementById('update-banner') as HTMLElement;
    expect(el.hidden).toBe(true);
    rerender(
      <Banner
        slot="update"
        kind="info"
        id="update-banner"
        data={mkData({ text: 'Hive 2.5.0 is available.', visible: true })}
      />,
    );
    expect(el.hidden).toBe(false);
    expect(el.querySelector('.hv-banner__text')?.textContent).toBe(
      'Hive 2.5.0 is available.',
    );
  });
});
