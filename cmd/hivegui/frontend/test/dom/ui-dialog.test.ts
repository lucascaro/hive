// @vitest-environment jsdom
//
// The dialog primitive. Four modals hand-rolled these behaviours with
// four slightly different rules; the risk in consolidating them is that
// one caller quietly loses one. Each is pinned here.
import { beforeEach, describe, expect, it, vi } from 'vitest';
import { anyModalOpen, unregisterModal } from '../../src/app/modals/registry';
import { dialog } from '../../src/ui/dialog';

describe('dialog()', () => {
  // The registry is module state shared by every test in the run, and
  // dialog() registers unconditionally. Without this, anyModalOpen()
  // answers for whatever another file left open — the assertion below
  // passed only because the files happened to run in a lucky order
  // (reproducible with --sequence.shuffle --sequence.seed=7).
  const built: HTMLElement[] = [];
  const mk = (spec: Parameters<typeof dialog>[0]) => {
    const d = dialog(spec);
    built.push(d.el);
    return d;
  };

  beforeEach(() => {
    for (const el of built.splice(0)) unregisterModal(el);
    document.body.replaceChildren();
  });

  it('renders the id, aria contract and starts hidden', () => {
    const d = mk({ id: 'demo', title: 'Demo', onClose: () => {} });
    document.body.append(d.el);
    expect(d.el.id).toBe('demo');
    expect(d.el.getAttribute('role')).toBe('dialog');
    expect(d.el.getAttribute('aria-modal')).toBe('true');
    const labelledBy = d.el.getAttribute('aria-labelledby');
    expect(document.getElementById(labelledBy ?? '')?.textContent).toBe('Demo');
    expect(d.el.classList.contains('hidden')).toBe(true);
    expect(d.isOpen()).toBe(false);
  });

  it('registers with the modal registry so the focus pipeline sees it', () => {
    const d = mk({ id: 'demo2', title: 'Demo', onClose: () => {} });
    document.body.append(d.el);
    expect(anyModalOpen()).toBe(false);
    d.show();
    expect(anyModalOpen()).toBe(true);
    d.hide();
    expect(anyModalOpen()).toBe(false);
  });

  it('calls onClose for Escape, the close button and the backdrop', () => {
    const onClose = vi.fn();
    const d = mk({ id: 'demo3', title: 'Demo', onClose });
    document.body.append(d.el);
    d.show();

    const esc = new KeyboardEvent('keydown', {
      key: 'Escape',
      bubbles: true,
      cancelable: true,
    });
    d.panel.dispatchEvent(esc);
    expect(onClose).toHaveBeenCalledTimes(1);
    // Consumed: keyboard.ts's window handler would otherwise see an
    // already-hidden dialog and spend the same Escape on what is behind it.
    expect(esc.defaultPrevented).toBe(true);

    d.el.querySelector<HTMLElement>('.hv-dialog__close')?.click();
    expect(onClose).toHaveBeenCalledTimes(2);

    d.el.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    d.el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(onClose).toHaveBeenCalledTimes(3);
  });

  it('ignores a click that only ENDS on the backdrop', () => {
    // A text-selection drag that starts in an input and releases outside
    // the panel dispatches its click on the backdrop. Closing on that
    // discards the user's draft mid-edit. Both ends must be the backdrop.
    const onClose = vi.fn();
    const d = mk({ id: 'demo4', title: 'Demo', onClose });
    document.body.append(d.el);
    d.show();
    d.panel.dispatchEvent(new MouseEvent('mousedown', { bubbles: true }));
    d.el.dispatchEvent(new MouseEvent('click', { bubbles: true }));
    expect(onClose).not.toHaveBeenCalled();
  });

  it('places body, actions and hints', () => {
    const b = document.createElement('p');
    const a = document.createElement('button');
    const k = document.createElement('kbd');
    const d = mk({
      id: 'demo5',
      title: 'Demo',
      size: 'lg',
      body: [b, null],
      actions: [a],
      hints: [k],
      onClose: () => {},
    });
    expect(d.body.contains(b)).toBe(true);
    expect(d.footer.contains(a)).toBe(true);
    expect(d.footer.contains(k)).toBe(true);
    expect(d.panel.dataset.size).toBe('lg');
  });
});
