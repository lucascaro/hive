// The link provider's two jobs: only underline paths that exist (the
// answer comes from Go), and only activate on the platform modifier —
// a plain click in a terminal still selects text.

import { beforeEach, describe, expect, it, vi } from 'vitest';

const OpenFile = vi.fn(async () => '');
const ResolveFilePaths = vi.fn(async (_base: string, c: string[]) =>
  c.map(() => ''),
);
const flash = vi.fn();

vi.mock('../../src/bridge.js', () => ({
  OpenFile: (...args: unknown[]) =>
    (OpenFile as unknown as (...a: unknown[]) => Promise<string>)(...args),
  ResolveFilePaths: (...args: unknown[]) =>
    (ResolveFilePaths as unknown as (...a: unknown[]) => Promise<string[]>)(
      ...args,
    ),
}));
vi.mock('../../src/app/dom.js', () => ({
  reportFailure: (what: string) => (err: unknown) => flash(what, err),
}));

import {
  createFileLinkProvider,
  isHiveFileLink,
  openFileLink,
} from '../../src/app/file-link-provider.js';

// A terminal stub with just the buffer surface the provider reads.
// Lines are given as (text, isWrapped). Characters are one cell wide
// unless listed in `wide`, which models a CJK glyph or emoji: it owns
// two columns, and the second reports width 0.
function fakeTerm(
  lines: [string, boolean][],
  cols: number,
  wide: string[] = [],
) {
  const cellsFor = (text: string) => {
    const out: { chars: string; width: number }[] = [];
    for (const ch of text) {
      out.push({ chars: ch, width: wide.includes(ch) ? 2 : 1 });
      if (wide.includes(ch)) out.push({ chars: '', width: 0 });
    }
    while (out.length < cols) out.push({ chars: ' ', width: 1 });
    return out;
  };
  return {
    cols,
    buffer: {
      active: {
        getNullCell: () => ({
          chars: '',
          width: 1,
          getChars() {
            return this.chars;
          },
          getWidth() {
            return this.width;
          },
        }),
        getLine: (y: number) => {
          const l = lines[y];
          if (!l) return undefined;
          const cells = cellsFor(l[0]);
          return {
            isWrapped: l[1],
            length: cells.length,
            translateToString: () => l[0],
            getCell: (x: number, cell: { chars: string; width: number }) => {
              const c = cells[x];
              if (!c) return undefined;
              cell.chars = c.chars;
              cell.width = c.width;
              return cell;
            },
          };
        },
      },
    },
  } as never;
}

// jsdom's navigator is not a Mac, so cmdOrCtrl() looks for Ctrl here.
// That is the point of the helper: the same code path is ⌘ on macOS
// and Ctrl on Windows and Linux, and the tests exercise whichever the
// environment reports.
function click(mods: { mod?: boolean; shift?: boolean } = {}) {
  return new MouseEvent('click', {
    ctrlKey: !!mods.mod,
    shiftKey: !!mods.shift,
  });
}

function provide(term: never, line: number) {
  const provider = createFileLinkProvider(term, () => '/base');
  return new Promise<
    ReturnType<typeof isHiveFileLink> extends never ? never : any[] | undefined
  >((resolve) => {
    provider.provideLinks(line, (links) => resolve(links as never));
  });
}

beforeEach(() => {
  OpenFile.mockClear();
  ResolveFilePaths.mockClear();
  flash.mockClear();
});

describe('createFileLinkProvider', () => {
  it('underlines only the paths Go says exist', async () => {
    ResolveFilePaths.mockImplementationOnce(async (_b, c) =>
      c.map((p) => (p === 'src/foo.ts' ? '/base/src/foo.ts' : '')),
    );
    const term = fakeTerm([['see src/foo.ts and nope/missing.ts', false]], 80);
    const links = await provide(term, 1);
    expect(links?.map((l) => l.text)).toEqual(['src/foo.ts']);
    expect(links?.every(isHiveFileLink)).toBe(true);
  });

  it('returns nothing when no candidate resolves', async () => {
    const term = fakeTerm([['see nope/missing.ts', false]], 80);
    expect(await provide(term, 1)).toBeUndefined();
  });

  it('asks Go once per hovered line, batching the candidates', async () => {
    ResolveFilePaths.mockImplementationOnce(async (_b, c) => c.map(() => ''));
    const term = fakeTerm([['a/one.ts b/two.ts c/three.ts', false]], 80);
    await provide(term, 1);
    expect(ResolveFilePaths).toHaveBeenCalledTimes(1);
    expect(ResolveFilePaths.mock.calls[0][1]).toEqual([
      'a/one.ts',
      'b/two.ts',
      'c/three.ts',
    ]);
  });

  it('skips lines with no path-shaped text without calling Go', async () => {
    const term = fakeTerm([['the quick brown fox', false]], 80);
    expect(await provide(term, 1)).toBeUndefined();
    expect(ResolveFilePaths).not.toHaveBeenCalled();
  });

  // A long path is wrapped across rows by xterm. The provider must
  // rebuild the logical line and map the offsets back to cells, or the
  // underline lands on the wrong characters.
  it('reassembles a wrapped line and maps the range across rows', async () => {
    ResolveFilePaths.mockImplementationOnce(async () => ['/base/aaaa/bb.ts']);
    const term = fakeTerm(
      [
        ['xx aaaa/', false],
        ['bb.ts:7', true],
      ],
      8,
    );
    // Hover the continuation row; the link still covers both rows.
    const links = await provide(term, 2);
    expect(links).toHaveLength(1);
    expect(links?.[0].range).toEqual({
      start: { x: 4, y: 1 },
      end: { x: 7, y: 2 },
    });
  });

  // A CJK glyph or emoji owns two columns, so deriving x from the
  // string offset puts the underline on the wrong cells — by one column
  // per wide glyph to its left.
  it('maps offsets through wide glyphs', async () => {
    ResolveFilePaths.mockImplementationOnce(async () => ['/base/a/b.ts']);
    const term = fakeTerm([['日本 a/b.ts', false]], 40, ['日', '本']);
    const links = await provide(term, 1);
    // '日'(1-2) '本'(3-4) ' '(5) then a/b.ts starts at column 6.
    expect(links?.[0].range).toEqual({
      start: { x: 6, y: 1 },
      end: { x: 11, y: 1 },
    });
  });

  // An astral-plane emoji is ONE code point but TWO UTF-16 units, and
  // the candidate offsets come from matchAll, which counts units. A
  // cells[] built by iterating code points is shorter than the text it
  // indexes, so every link to the right of one lands a column early.
  it('maps offsets through an astral-plane emoji', async () => {
    ResolveFilePaths.mockImplementationOnce(async () => ['/base/a/b.ts']);
    const term = fakeTerm([['\u{1F600} a/b.ts', false]], 40, ['\u{1F600}']);
    const links = await provide(term, 1);
    // emoji(1-2) ' '(3) then a/b.ts occupies columns 4-9.
    expect(links?.[0].range).toEqual({
      start: { x: 4, y: 1 },
      end: { x: 9, y: 1 },
    });
  });

  it('gives up on an absurdly long unwrapped line rather than walking it', async () => {
    const huge: [string, boolean][] = [];
    for (let i = 0; i < 300; i++) huge.push(['x'.repeat(200), i > 0]);
    const term = fakeTerm(huge, 200);
    expect(await provide(term, 300)).toBeUndefined();
    expect(ResolveFilePaths).not.toHaveBeenCalled();
  });

  // Moving along a row re-asks for the same line; each miss is an IPC
  // round-trip plus a stat per candidate.
  it('memoises the resolution per line', async () => {
    ResolveFilePaths.mockImplementation(async () => ['/base/src/foo.ts']);
    const term = fakeTerm([['see src/foo.ts', false]], 80);
    const provider = createFileLinkProvider(term, () => '/base');
    const ask = () =>
      new Promise((r) => provider.provideLinks(1, (l) => r(l as never)));
    await ask();
    await ask();
    await ask();
    expect(ResolveFilePaths).toHaveBeenCalledTimes(1);
  });

  // The pointer crossing a row re-asks within milliseconds, before the
  // first call has resolved: caching only settled answers would fire a
  // duplicate round-trip every time.
  it('shares one in-flight resolution between overlapping hovers', async () => {
    let release: (v: string[]) => void = () => {};
    ResolveFilePaths.mockImplementationOnce(
      () => new Promise<string[]>((r) => (release = r)),
    );
    const term = fakeTerm([['see src/foo.ts', false]], 80);
    const provider = createFileLinkProvider(term, () => '/base');
    const ask = () =>
      new Promise((r) => provider.provideLinks(1, (l) => r(l as never)));
    const a = ask();
    const b = ask();
    release(['/base/src/foo.ts']);
    await Promise.all([a, b]);
    expect(ResolveFilePaths).toHaveBeenCalledTimes(1);
  });

  it('does not cache a failed lookup', async () => {
    ResolveFilePaths.mockRejectedValueOnce(new Error('bridge down') as never);
    ResolveFilePaths.mockImplementationOnce(async () => ['/base/src/foo.ts']);
    const term = fakeTerm([['see src/foo.ts', false]], 80);
    const provider = createFileLinkProvider(term, () => '/base');
    const ask = () =>
      new Promise((r) => provider.provideLinks(1, (l) => r(l as never)));
    expect(await ask()).toBeUndefined();
    const links = (await ask()) as { text: string }[] | undefined;
    expect(links?.map((l) => l.text)).toEqual(['src/foo.ts']);
  });

  it('passes the parsed line and col when activated', async () => {
    ResolveFilePaths.mockImplementationOnce(async () => ['/base/src/foo.ts']);
    const term = fakeTerm([['src/foo.ts:12:5', false]], 80);
    const links = await provide(term, 1);
    links?.[0].activate(click({ mod: true }), 'src/foo.ts');
    expect(OpenFile).toHaveBeenCalledWith('/base', 'src/foo.ts', 12, 5, false);
  });
});

describe('openFileLink', () => {
  it('does nothing without the modifier', () => {
    expect(openFileLink(click(), '/base', 'src/foo.ts')).toBe(false);
    expect(OpenFile).not.toHaveBeenCalled();
  });

  it('opens with the OS default on cmd-click', () => {
    expect(openFileLink(click({ mod: true }), '/base', 'src/foo.ts', 3)).toBe(
      true,
    );
    expect(OpenFile).toHaveBeenCalledWith('/base', 'src/foo.ts', 3, 0, false);
  });

  it('opens in the editor on shift-cmd-click', () => {
    openFileLink(
      click({ mod: true, shift: true }),
      '/base',
      'src/foo.ts',
      3,
      4,
    );
    expect(OpenFile).toHaveBeenCalledWith('/base', 'src/foo.ts', 3, 4, true);
  });

  it('reports a failure to the status bar rather than failing silently', async () => {
    OpenFile.mockRejectedValueOnce(new Error('boom') as never);
    openFileLink(click({ mod: true }), '/base', 'src/foo.ts');
    await Promise.resolve();
    await Promise.resolve();
    expect(flash).toHaveBeenCalled();
    expect(flash.mock.calls[0][0]).toBe('open file');
  });
});
