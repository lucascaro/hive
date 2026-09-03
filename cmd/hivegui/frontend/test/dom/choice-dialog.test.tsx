// @vitest-environment jsdom
//
// The choice dialog (src/app/modals/choice-dialog.ts +
// components/modals/ChoiceDialog.tsx).
//
// The bug this phase designs away: the dialog used to be BUILT per
// question and appended to <body>, which meant closing it had to
// unregister the element as well as remove it. A detached element has no
// `hidden` class, so a forgotten unregister made "does a modal own the
// keyboard?" answer true forever and permanently stranded the keyboard —
// every keystroke swallowed, with nothing on screen to explain why.
//
// The root is static now and its visibility is one store field, so the
// invariant is checkable in one line: after an answer, nothing claims
// the keyboard. That is what most of this file asserts.
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { act, render, screen, cleanup } from '@testing-library/react';
import { ChoiceDialog } from '../../src/components/modals/ChoiceDialog.js';
import {
  openChoiceDialog,
  choiceDialogOpen,
  dismissChoiceDialog,
} from '../../src/app/modals/choice-dialog.js';
import { anyModalOpen, resetStore } from '../../src/store/store.js';

const ROOT = `<div id="choice-dialog" class="hv-dialog hidden"
  role="alertdialog" aria-modal="true" aria-labelledby="choice-dialog-title"></div>`;

function mount() {
  document.body.innerHTML = ROOT;
  const root = document.getElementById('choice-dialog') as HTMLElement;
  // Rendered INTO the root, as main.ts's island does: the component
  // reaches its own subtree through that element (the focus query, the
  // shell's listeners), which a detached RTL container would not be.
  render(<ChoiceDialog root={root} />, { container: root });
  return root;
}

const SPEC = {
  title: 'Delete this worktree?',
  detail: '/tmp/wt/feature',
  bullets: ['It has uncommitted changes.'],
  note: 'Keeping the branch leaves its commits recoverable.',
  choices: [
    { label: 'Cancel', value: 'cancel' },
    { label: 'Delete, keep branch', value: 'keep-branch' },
    { label: 'Delete + local branch', value: 'both', danger: true },
  ],
};

const answer = (value: string) =>
  act(() => {
    document
      .querySelector<HTMLButtonElement>(`button[data-choice="${value}"]`)
      ?.click();
  });

beforeEach(() => {
  resetStore();
});

afterEach(() => {
  // The resolver lives in module scope so it can outlive the component,
  // which means resetStore() does not clear it — an unanswered question
  // would hold it into the next test. dismiss is the same door the app
  // uses.
  dismissChoiceDialog();
  cleanup();
});

describe('rendering', () => {
  it('is hidden until a question is asked', () => {
    const root = mount();
    expect(root.classList.contains('hidden')).toBe(true);
    expect(root.querySelector('.hv-dialog__panel')).toBeNull();
    // `.choice-dialog` means "a question is on screen" — the e2e specs
    // count it to assert there is none.
    expect(root.classList.contains('choice-dialog')).toBe(false);
  });

  it('renders the question, its detail, bullets and note', async () => {
    const root = mount();
    act(() => {
      void openChoiceDialog(SPEC);
    });

    expect(root.classList.contains('hidden')).toBe(false);
    expect(root.classList.contains('choice-dialog')).toBe(true);
    expect(screen.getByText('Delete this worktree?')).toBeTruthy();
    expect(root.querySelector('.choice-dialog-detail')?.textContent).toBe(
      '/tmp/wt/feature',
    );
    expect(
      [...root.querySelectorAll('.choice-dialog-bullets li')].map(
        (li) => li.textContent,
      ),
    ).toEqual(['It has uncommitted changes.']);
    expect(root.querySelector('.choice-dialog-note')?.textContent).toContain(
      'recoverable',
    );
    // No close button: an alertdialog with a question has no neutral
    // "dismiss" — the safe choice is a button of its own.
    expect(root.querySelector('.hv-dialog__close')).toBeNull();
  });

  it('marks the destructive choice, in class as well as kind', () => {
    mount();
    act(() => {
      void openChoiceDialog(SPEC);
    });
    const danger = document.querySelector<HTMLButtonElement>(
      'button[data-choice="both"]',
    );
    expect(danger?.classList.contains('danger')).toBe(true);
    expect(danger?.dataset.kind).toBe('danger');
    expect(
      document
        .querySelector('button[data-choice="cancel"]')
        ?.classList.contains('danger'),
    ).toBe(false);
  });

  it('omits the optional lines a spec leaves out', () => {
    const root = mount();
    act(() => {
      void openChoiceDialog({
        title: 'Close this session?',
        choices: [{ label: 'Cancel', value: 'cancel' }],
      });
    });
    expect(root.querySelector('.choice-dialog-detail')).toBeNull();
    expect(root.querySelector('.choice-dialog-bullets')).toBeNull();
    expect(root.querySelector('.choice-dialog-note')).toBeNull();
  });

  it('focuses the safe choice, so a stray Enter destroys nothing', () => {
    mount();
    act(() => {
      void openChoiceDialog(SPEC);
    });
    expect((document.activeElement as HTMLElement)?.dataset.choice).toBe(
      'cancel',
    );
  });
});

describe('answering', () => {
  it('resolves with the pressed choice and cleans up completely', async () => {
    const root = mount();
    let settled: string | null = null;
    act(() => {
      void openChoiceDialog(SPEC).then((v) => {
        settled = v;
      });
    });
    expect(anyModalOpen()).toBe(true);

    answer('both');
    await act(async () => {});

    expect(settled).toBe('both');
    // The three things a stranded keyboard is made of: the promise
    // pending, the store still open, the root still visible.
    expect(choiceDialogOpen()).toBe(false);
    expect(anyModalOpen()).toBe(false);
    expect(root.classList.contains('hidden')).toBe(true);
    expect(root.classList.contains('choice-dialog')).toBe(false);
    expect(root.querySelector('.hv-dialog__panel')).toBeNull();
  });

  it('dismisses to the FIRST choice, which is the safe one', async () => {
    mount();
    let settled: string | null = null;
    act(() => {
      void openChoiceDialog(SPEC).then((v) => {
        settled = v;
      });
    });
    act(() => {
      dismissChoiceDialog();
    });
    await act(async () => {});
    expect(settled).toBe('cancel');
  });

  it('reports whether there was anything to dismiss', () => {
    mount();
    expect(dismissChoiceDialog()).toBe(false);
    act(() => {
      void openChoiceDialog(SPEC);
    });
    let dismissed = false;
    act(() => {
      dismissed = dismissChoiceDialog();
    });
    expect(dismissed).toBe(true);
  });

  it('never leaves a second question stacked behind the first', async () => {
    mount();
    let first: string | null = null;
    act(() => {
      void openChoiceDialog(SPEC).then((v) => {
        first = v;
      });
    });
    act(() => {
      void openChoiceDialog({
        title: 'Something else?',
        choices: [
          { label: 'No', value: 'no' },
          { label: 'Yes', value: 'yes' },
        ],
      });
    });
    await act(async () => {});

    // The first promise settled to its safe answer rather than hanging
    // forever behind a dialog the user can no longer see.
    expect(first).toBe('cancel');
    expect(screen.getByText('Something else?')).toBeTruthy();
    expect(document.querySelectorAll('.hv-dialog__panel').length).toBe(1);

    // Answer it. The resolver lives in module scope — it outlives the
    // component and resetStore() does not clear it — so a question left
    // hanging here would still be holding the module's `settle` when the
    // next test opens one.
    answer('no');
    await act(async () => {});
    expect(choiceDialogOpen()).toBe(false);
  });

  it('remounts per question, so a re-ask starts on its own safe choice', async () => {
    mount();
    act(() => {
      void openChoiceDialog(SPEC);
    });
    answer('cancel');
    await act(async () => {});
    act(() => {
      void openChoiceDialog({
        title: 'Delete the branch?',
        choices: [
          { label: 'Keep it', value: 'cancel' },
          { label: 'Delete branch', value: 'local', danger: true },
        ],
      });
    });
    expect(
      (document.activeElement as HTMLElement)?.textContent?.trim(),
    ).toContain('Keep it');
  });
});

describe('focus restoration', () => {
  it('puts focus back on the control that raised the question', async () => {
    document.body.innerHTML = `${ROOT}<button id="opener">Delete</button>`;
    const root = document.getElementById('choice-dialog') as HTMLElement;
    render(<ChoiceDialog root={root} />, { container: root });
    const opener = document.getElementById('opener') as HTMLButtonElement;
    opener.focus();

    act(() => {
      void openChoiceDialog(SPEC);
    });
    answer('cancel');
    await act(async () => {});

    expect(document.activeElement).toBe(opener);
  });

  it('leaves focus alone when the opener is gone', async () => {
    document.body.innerHTML = `${ROOT}<button id="opener">Delete</button>`;
    const root = document.getElementById('choice-dialog') as HTMLElement;
    render(<ChoiceDialog root={root} />, { container: root });
    const opener = document.getElementById('opener') as HTMLButtonElement;
    opener.focus();

    act(() => {
      void openChoiceDialog(SPEC);
    });
    // Answering the question is often what removes the row the opener
    // lives on — focusing a detached node is how the keyboard gets
    // stranded.
    opener.remove();
    answer('cancel');
    await act(async () => {});

    expect(document.activeElement).not.toBe(opener);
    expect(document.body.contains(document.activeElement)).toBe(true);
  });
});

describe('the keyboard is never stranded', () => {
  it('answering leaves nothing claiming it, however the dialog closed', async () => {
    const root = mount();
    for (const close of [
      () => answer('cancel'),
      () => act(() => void dismissChoiceDialog()),
    ]) {
      act(() => {
        void openChoiceDialog(SPEC);
      });
      expect(anyModalOpen()).toBe(true);
      close();
      await act(async () => {});
      expect(anyModalOpen()).toBe(false);
      expect(root.classList.contains('hidden')).toBe(true);
    }
  });
});

vi.mock('../../src/app/session-term.js', () => ({}));
