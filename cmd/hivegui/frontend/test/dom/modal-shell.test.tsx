// @vitest-environment jsdom
//
// The shared modal shell (src/components/modals/ModalShell.tsx): the
// markup contract ui/dialog.ts established, and the three gestures it
// owns — Escape, the backdrop, and Tab containment. The `hidden` class
// is deliberately NOT here: the island that mounts the shell owns it
// (see settings.test.tsx), because it has to be right in the frame the
// shell is no longer mounted in.
import { describe, it, expect, vi, beforeEach } from 'vitest';
import type { Mock } from 'vitest';
import { fireEvent, render } from '@testing-library/react';
import { ModalShell } from '../../src/components/modals/ModalShell.js';

let root: HTMLElement;
// Declared with its exact signature: vitest's bare Mock won't satisfy
// the `() => void` the shell's prop asks for.
let onClose: Mock<() => void>;

function mount(children?: React.ReactNode) {
  return render(
    <ModalShell
      id="demo"
      root={root}
      title="Demo"
      onClose={onClose}
      hints={[
        { keys: '[esc]', label: 'cancel' },
        { keys: '[enter]', label: 'save' },
      ]}
      actions={
        <button type="button" id="demo-ok">
          OK
        </button>
      }
    >
      {children ?? <input type="text" aria-label="first" />}
    </ModalShell>,
    { container: root },
  );
}

beforeEach(() => {
  document.body.innerHTML = '<div id="app"></div>';
  root = document.createElement('div');
  root.id = 'demo';
  root.className = 'hv-dialog';
  document.getElementById('app')?.appendChild(root);
  onClose = vi.fn();
});

describe('modal shell markup', () => {
  it('renders the dialog structure the primitive established', () => {
    mount();
    const panel = root.querySelector('#demo-panel');
    expect(panel?.className).toBe('hv-dialog__panel');
    expect(panel?.getAttribute('data-size')).toBe('md');
    expect(root.querySelector('#demo-title')?.textContent).toBe('Demo');
    expect(
      root.querySelector('.hv-dialog__title-suffix'),
      'the suffix slot rides inside the h3 so the accessible name stays one string',
    ).not.toBeNull();
    expect(root.querySelector('#demo-close')?.className).toContain(
      'hv-dialog__close',
    );
    expect(root.querySelector('.hv-dialog__body input')).not.toBeNull();
    expect(root.querySelector('.hv-dialog__actions #demo-ok')).not.toBeNull();
  });

  // AGENTS.md, Feedback on Action: every overlay must display the
  // bindings that confirm and cancel it.
  it('shows its confirm/cancel key hints', () => {
    mount();
    const hints = [...root.querySelectorAll('.hv-dialog__hints .hv-kbd')].map(
      (k) => k.textContent,
    );
    expect(hints).toEqual(['[esc]', '[enter]']);
    expect(root.querySelector('.hv-dialog__hints')?.textContent).toContain(
      'cancel',
    );
  });

  it('closes from the header button', () => {
    mount();
    fireEvent.click(root.querySelector('#demo-close') as HTMLElement);
    expect(onClose).toHaveBeenCalledTimes(1);
  });
});

describe('modal shell gestures', () => {
  it('closes on Escape and consumes the event', () => {
    mount();
    const seen = vi.fn();
    window.addEventListener('keydown', seen);
    const delivered = root.dispatchEvent(
      new KeyboardEvent('keydown', {
        key: 'Escape',
        bubbles: true,
        cancelable: true,
      }),
    );
    window.removeEventListener('keydown', seen);
    expect(onClose).toHaveBeenCalledTimes(1);
    expect(delivered, 'Escape must be preventDefaulted').toBe(false);
    expect(
      seen,
      'a second handler must not spend the same Escape',
    ).not.toHaveBeenCalled();
  });

  // Both ends of the gesture must land on the backdrop: a text-selection
  // drag that starts inside an input and releases outside the panel
  // dispatches its click on the backdrop, and closing there discards the
  // whole draft mid-edit.
  it('closes on a backdrop click but not on a drag that merely ends there', () => {
    mount();
    fireEvent.mouseDown(root);
    fireEvent.click(root);
    expect(onClose).toHaveBeenCalledTimes(1);

    onClose.mockClear();
    fireEvent.mouseDown(root.querySelector('input') as HTMLElement);
    fireEvent.click(root);
    expect(onClose).not.toHaveBeenCalled();
  });

  it('does not close on a click inside the panel', () => {
    mount();
    const panel = root.querySelector('#demo-panel') as HTMLElement;
    fireEvent.mouseDown(panel);
    fireEvent.click(panel);
    expect(onClose).not.toHaveBeenCalled();
  });

  // The trap is acquired while the shell is mounted and released with
  // it: an untrapped modal's next Tab stop is a hidden terminal's
  // textarea, so keystrokes leak into a session the user cannot see.
  it('wraps Tab at the end of the dialog and releases the trap on unmount', () => {
    const view = mount();
    const last = root.querySelector('#demo-ok') as HTMLElement;
    const close = root.querySelector('#demo-close') as HTMLElement;
    last.focus();
    const tab = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true,
    });
    root.dispatchEvent(tab);
    expect(tab.defaultPrevented).toBe(true);
    expect(document.activeElement).toBe(close);

    view.unmount();
    const after = new KeyboardEvent('keydown', {
      key: 'Tab',
      bubbles: true,
      cancelable: true,
    });
    root.dispatchEvent(after);
    expect(after.defaultPrevented).toBe(false);
  });
});
