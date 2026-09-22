import { describe, expect, it } from 'vitest';
import {
  findPathCandidates,
  isFileUri,
  parseFileUri,
} from '../../src/lib/file-links.js';

// The detector is deliberately generous — Go's existence check is what
// decides whether a candidate becomes an underlined link. These tests
// pin the shapes agents and tools actually print, and the offsets,
// which is what maps a click back to the right cells.
describe('findPathCandidates', () => {
  const paths = (text: string) => findPathCandidates(text).map((c) => c.path);

  it('finds repo-relative, absolute, home and dot paths', () => {
    expect(paths('see src/app/foo.ts for this')).toEqual(['src/app/foo.ts']);
    expect(paths('wrote /tmp/out.log')).toEqual(['/tmp/out.log']);
    expect(paths('open ~/notes.md now')).toEqual(['~/notes.md']);
    expect(paths('run ./scripts/test.sh')).toEqual(['./scripts/test.sh']);
    expect(paths('from ../other/pkg.go')).toEqual(['../other/pkg.go']);
  });

  it('finds a bare name with an extension', () => {
    expect(paths('edit README.md please')).toEqual(['README.md']);
  });

  it('finds a Windows absolute path', () => {
    expect(paths('at C:\\src\\main.go here')).toEqual(['C:\\src\\main.go']);
  });

  it('parses :line and :line:col', () => {
    const [a] = findPathCandidates('src/foo.ts:12');
    expect([a.path, a.line, a.col]).toEqual(['src/foo.ts', 12, undefined]);
    const [b] = findPathCandidates('src/foo.ts:12:5');
    expect([b.path, b.line, b.col]).toEqual(['src/foo.ts', 12, 5]);
    // The MSVC `(12,5)` spelling is not parsed — the comma ends the
    // candidate — but the path itself is still found.
    expect(paths('src/foo.ts(12,5)')).toEqual(['src/foo.ts(12']);
  });

  it('strips prose punctuation but keeps the position', () => {
    const [a] = findPathCandidates('see src/foo.ts:12.');
    expect([a.path, a.line]).toEqual(['src/foo.ts', 12]);
    expect(paths('(src/foo.ts)')).toEqual(['src/foo.ts']);
    expect(paths('files: a/b.ts, c/d.ts;')).toEqual(['a/b.ts', 'c/d.ts']);
  });

  it('ignores URLs and flags', () => {
    expect(paths('https://example.com/a/b.html')).toEqual([]);
    expect(paths('pass --config=x or -v')).toEqual([]);
  });

  it('ignores ordinary prose', () => {
    expect(paths('the quick brown fox jumped')).toEqual([]);
  });

  it('reports offsets covering the whole match', () => {
    const text = 'see src/foo.ts:12 now';
    const [c] = findPathCandidates(text);
    expect(text.slice(c.start, c.end)).toBe('src/foo.ts:12');
  });

  it('reports offsets after stripped punctuation', () => {
    const text = 'see (src/foo.ts).';
    const [c] = findPathCandidates(text);
    expect(text.slice(c.start, c.end)).toBe('src/foo.ts');
  });
});

describe('parseFileUri', () => {
  it('accepts a local file URI', () => {
    expect(parseFileUri('file:///tmp/out.log')).toBe('/tmp/out.log');
    expect(parseFileUri('file://localhost/tmp/out.log')).toBe('/tmp/out.log');
  });

  it('percent-decodes', () => {
    expect(parseFileUri('file:///tmp/a%20b.md')).toBe('/tmp/a b.md');
  });

  // A remote host is not ours to open, and resolving it locally would
  // silently open a different file with the same name.
  it('rejects a remote host', () => {
    expect(parseFileUri('file://evil.example.com/etc/passwd')).toBeNull();
  });

  // `file:////host/share` parses with an EMPTY hostname — the host
  // check passes and the path is a UNC path, which dials the host and
  // on Windows hands over the user's NTLM hash.
  it('rejects an extra leading slash smuggling a UNC path', () => {
    expect(parseFileUri('file:////evil.example.com/share/a.txt')).toBeNull();
    expect(parseFileUri('file://///evil/share/a')).toBeNull();
  });

  // Without stripping the leading slash this is '/C:/src/main.go',
  // which is neither absolute nor relative, and every OSC 8 file link
  // on Windows fails to open.
  it('strips the leading slash from a drive-letter path', () => {
    expect(parseFileUri('file:///C:/src/main.go')).toBe('C:/src/main.go');
    expect(parseFileUri('file:///c:/src/main.go')).toBe('c:/src/main.go');
    // A lone colon in a normal path is not a drive letter.
    expect(parseFileUri('file:///tmp/a:b.md')).toBe('/tmp/a:b.md');
  });

  it('rejects other schemes and garbage', () => {
    expect(parseFileUri('https://example.com/x')).toBeNull();
    expect(parseFileUri('not a uri')).toBeNull();
  });
});

describe('isFileUri', () => {
  it('matches case-insensitively', () => {
    expect(isFileUri('FILE:///tmp/x')).toBe(true);
    expect(isFileUri('https://x/y')).toBe(false);
  });
});
