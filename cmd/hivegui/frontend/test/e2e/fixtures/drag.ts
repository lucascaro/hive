// Playwright-side drag-to-reorder driver, shared by every spec that drags a
// list row (ordering.spec.ts for sidebar sessions, settings.spec.ts for
// pinned agents). Both lists run through the same app code
// (lib/drag-row.ts + lib/drag-placeholder.ts), so they share one driver.
//
// The drag is driven by dispatched DragEvents sharing one DataTransfer
// rather than by locator.dragTo(): headless Chromium does not synthesise
// native HTML5 drag from mouse input here (verified — no dragstart fires),
// and the app's handlers are what these tests are about. Everything past
// the event dispatch is real: real layout, real CSS, real reorder call.
// What this cannot cover is the browser's own decision to *begin* a drag —
// notably that hiding the source element must be deferred a tick or the
// drag is cancelled. That one needs a human in the built app.
import type { Page } from '@playwright/test';

// Starts a drag on the row matching `selector` and waits for beginDrag's
// deferred swap (row out of the flow, placeholder in) to land.
export async function dragStart(page: Page, selector: string) {
  await page.evaluate((sel) => {
    const w = window as unknown as { __dt?: DataTransfer };
    w.__dt = new DataTransfer();
    const row = document.querySelector<HTMLElement>(sel);
    if (!row) throw new Error(`no row ${sel}`);
    row.dispatchEvent(
      new DragEvent('dragstart', {
        bubbles: true,
        cancelable: true,
        dataTransfer: w.__dt,
      }),
    );
  }, selector);
  await page.waitForFunction(
    (sel) =>
      document.querySelector(sel)?.classList.contains('dragging') ?? false,
    selector,
  );
}

// Both dragover and drop carry the cursor's y, which is what decides the
// above/below half; the two must agree or the drop lands somewhere the
// placeholder never showed.
export async function dragEvent(
  page: Page,
  type: 'dragover' | 'drop',
  selector: string,
  above: boolean,
) {
  await page.evaluate(
    ({ kind, sel, top }) => {
      const w = window as unknown as { __dt?: DataTransfer };
      const row = document.querySelector<HTMLElement>(sel);
      if (!row) throw new Error(`no row ${sel}`);
      const r = row.getBoundingClientRect();
      row.dispatchEvent(
        new DragEvent(kind, {
          bubbles: true,
          cancelable: true,
          dataTransfer: w.__dt,
          clientY: top ? r.top + 2 : r.bottom - 2,
        }),
      );
    },
    { kind: type, sel: selector, top: above },
  );
}
