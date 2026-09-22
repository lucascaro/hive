// Finding file paths in terminal text.
//
// Pure module: no DOM, no bridge, unit-testable. The caller
// (app/file-link-provider.ts) asks Go which candidates actually exist
// — that check, not this regex, is what decides whether a path gets
// underlined. So this side is deliberately generous: a token that
// merely looks like a path costs one stat, and a token we fail to
// recognise costs the user a feature.
//
// Nothing here authorises anything. Go re-resolves every path it is
// asked to open (cmd/hivegui/file_open.go).

export interface PathCandidate {
  /** Offset of the first character of the whole match, including :line:col. */
  start: number;
  /** Offset one past the last character of the whole match. */
  end: number;
  /** The path part alone. */
  path: string;
  line?: number;
  col?: number;
}

// A path-shaped run of characters. Spaces are excluded: a path with a
// space in it is indistinguishable from two words in terminal output,
// and guessing wrong underlines whole sentences.
//
// Characters excluded from the run besides whitespace: quotes and
// backticks (they wrap paths), angle brackets and pipes (redirection),
// and the comma and semicolon (list separators). Parentheses and
// brackets are allowed inside — some real paths contain them — and
// trimmed at the edges afterwards.
const PATHISH = /[^\s"'`<>|,;]+/g;

// Trailing punctuation that is almost always prose, not part of the
// name: "see src/foo.ts." or "(src/foo.ts)".
const TRAILING = /[.,;:!?)\]}]+$/;
const LEADING = /^[([{]+/;

// :12 or :12:5 — the position suffix agents, compilers and linters
// print. Kept out of the path itself. The MSVC `(12,5)` spelling is
// deliberately not matched: the comma already ends a candidate (it
// separates list items far more often than it means a column).
const POSITION = /:(\d+)(?::(\d+))?$/;

// A token is path-shaped when it has a separator, starts with a
// relative or home marker, or is a bare name with an extension
// (README.md). A bare name is only a candidate because Go will say
// whether it exists.
const SEPARATOR = /[/\\]/;
const HOME_OR_RELATIVE = /^(?:~|\.\.?)[/\\]/;
const WINDOWS_ABS = /^[A-Za-z]:[\\/]/;
const BARE_WITH_EXT = /^[\w.@+-]+\.[A-Za-z0-9]{1,12}$/;

function looksLikePath(token: string): boolean {
  if (token.includes('://')) return false; // a URL; the web-links addon owns it
  if (token.startsWith('-')) return false; // a flag
  if (HOME_OR_RELATIVE.test(token) || WINDOWS_ABS.test(token)) return true;
  if (SEPARATOR.test(token)) return true;
  return BARE_WITH_EXT.test(token);
}

/**
 * findPathCandidates returns every path-shaped token in `text`, with
 * offsets into `text` so the caller can map them back to buffer cells.
 *
 * Offsets cover the position suffix too: clicking the `12` in
 * `foo.ts:12` should follow the same link as clicking the name.
 */
export function findPathCandidates(text: string): PathCandidate[] {
  const out: PathCandidate[] = [];
  for (const m of text.matchAll(PATHISH)) {
    const raw = m[0];
    const at = m.index ?? 0;

    // Trim wrapping punctuation, tracking the offsets as we go so a
    // click maps back to the right cells.
    const lead = LEADING.exec(raw)?.[0].length ?? 0;
    let token = raw.slice(lead);
    const start = at + lead;
    let end = start + token.length;

    // Prose punctuation first ("see src/foo.ts:12."), then the
    // position suffix — otherwise the trailing period hides the :12.
    const trail = TRAILING.exec(token)?.[0];
    if (trail) {
      token = token.slice(0, token.length - trail.length);
      end -= trail.length;
    }

    const pos = POSITION.exec(token);
    let line: number | undefined;
    let col: number | undefined;
    if (pos) {
      line = Number(pos[1]);
      col = pos[2] !== undefined ? Number(pos[2]) : undefined;
      token = token.slice(0, pos.index);
    }

    if (!token || !looksLikePath(token)) continue;
    out.push({ start, end, path: token, line, col });
  }
  return out;
}

/**
 * parseFileUri turns an OSC 8 `file://` URI into a local path, or
 * returns null when it names another machine — a remote host is not
 * ours to open, and resolving it locally would open the wrong file.
 */
export function parseFileUri(uri: string): string | null {
  let parsed: URL;
  try {
    parsed = new URL(uri);
  } catch {
    return null;
  }
  if (parsed.protocol !== 'file:') return null;
  if (parsed.hostname && parsed.hostname !== 'localhost') return null;
  let path: string;
  try {
    path = decodeURIComponent(parsed.pathname);
  } catch {
    return null;
  }
  // `file:////host/share/x` parses with an EMPTY hostname and a
  // pathname of `//host/share/x` — so the host check above passes and
  // the result is a UNC path. Opening one dials out to that host, which
  // on Windows hands over the user's NTLM hash before anything is even
  // read. An extra leading slash is never a local path; refuse it.
  if (path.startsWith('//')) return null;
  // A Windows drive path arrives as `/C:/src/main.go`. Left alone it is
  // neither absolute nor relative and every OSC 8 file link on Windows
  // fails to open.
  if (/^\/[A-Za-z]:[\\/]/.test(path)) path = path.slice(1);
  return path || null;
}

/** True when an OSC 8 link's URI targets a local file. */
export function isFileUri(uri: string): boolean {
  return uri.slice(0, 5).toLowerCase() === 'file:';
}
