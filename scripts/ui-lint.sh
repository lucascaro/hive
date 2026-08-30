#!/usr/bin/env bash
# UI literal lint. Rules (docs/design-docs/ui/README.md, principle 3):
#   hex      — raw #rgb/#rrggbb/#rrggbbaa outside src/theme/{tokens,themes}.css
#              (CSS comments are stripped before matching, so prose that
#              discusses a hex value doesn't trip the rule; a real
#              declaration with a trailing comment is still caught)
#   px-size  — font-size: <n>px outside src/theme/tokens.css
#   glyph    — a denylist of icon-shaped Unicode characters in
#              src/app/**/*.ts, src/style.css and index.html. Icons come
#              from the sprite via icon() now (docs/design-docs/ui/icons.md
#              > Rules); anything on this list is an unconverted call site.
#              This is deliberately NOT "any non-ASCII" — prose in comments
#              legitimately uses em dashes, curly quotes and arrows, and key
#              hints (⌘⇧⌥⌃⌫) are required by AGENTS.md.
# A trailing `/* ui-lint: allow */` (CSS) or `// ui-lint: allow` (TS) exempts a line.
# Exit 0 in warn mode; --strict exits 1 on any violation.
#
# Usage: ui-lint.sh [--strict] [path ...]
#   No paths -> lints the whole frontend tree (rule-specific defaults below).
#   Explicit paths -> all three rules (including glyph) are scoped to them,
#   so `ui-lint.sh --strict scripts/testdata/ui-lint/bad.css` is meaningful.
#
# Requires bash 4+ semantics are NOT assumed: this script is written to run
# under bash 3.2 (stock macOS /bin/bash) as well as bash 5.x (CI/Linux) —
# arrays are only ever expanded once populated, never while empty, since
# `"${arr[@]}"` on an empty array is an unbound-variable error under
# `set -u` on bash 3.2 (bash 5 tolerates it; don't rely on that).
#
# No rule here needs PCRE (`grep -P`) — the hex/px-size rules use plain ERE
# (`grep -E`) and the glyph rule is a fixed-character denylist matched with
# `grep -F`, so stock BSD grep (macOS) and GNU grep (Linux/CI) both work.
set -euo pipefail
cd "$(dirname "$0")/.."
FE=cmd/hivegui/frontend

strict=0
if [[ "${1:-}" == "--strict" ]]; then
  strict=1
  shift
fi
targets=("$@")
custom=1
if [[ ${#targets[@]} -eq 0 ]]; then
  custom=0
  targets=("$FE/src" "$FE/index.html")
fi
# Glyph rule defaults to src/app + src/style.css + index.html when no
# explicit targets are given, but honours explicit targets like the other
# two rules do — otherwise `ui-lint.sh --strict some/fixture.ts` would
# silently ignore the argument and scan the whole app tree instead.
glyph_targets=("${targets[@]}")
if [[ $custom -eq 0 ]]; then
  glyph_targets=("$FE/src/app" "$FE/src/style.css" "$FE/index.html")
fi

n=0
report() { echo "$1"; n=$((n + 1)); }

# Blanks out the contents of /* ... */ comments (state carries across
# lines) while preserving line numbers and line length, so a regex match
# against the output can never land inside a comment. Text before/after a
# comment on the same line is left untouched, so `color: #fff; /* #eee */`
# still flags the real #fff.
strip_css_comments() {
  awk '
  BEGIN { incomment = 0 }
  {
    n = length($0)
    out = ""
    for (i = 1; i <= n; i++) {
      c = substr($0, i, 1)
      if (incomment) {
        if (c == "*" && substr($0, i + 1, 1) == "/") { out = out "  "; i++; incomment = 0 }
        else out = out " "
      } else {
        if (c == "/" && substr($0, i + 1, 1) == "*") { out = out "  "; i++; incomment = 1 }
        else out = out c
      }
    }
    print out
  }'
}

while IFS= read -r line; do report "$line"; done < <(
  find "${targets[@]}" -name '*.css' 2>/dev/null | while IFS= read -r f; do
    strip_css_comments <"$f" | grep -nE '#[0-9a-fA-F]{3,8}\b' 2>/dev/null | cut -d: -f1 | while IFS= read -r ln; do
      printf '%s:%s:%s\n' "$f" "$ln" "$(sed -n "${ln}p" "$f")"
    done
  done | grep -v -e 'src/theme/tokens.css' -e 'src/theme/themes.css' -e 'ui-lint: allow' \
    | sed 's/^/hex: /' || true)

while IFS= read -r line; do report "$line"; done < <(
  grep -rnE --include='*.css' 'font-size:\s*[0-9.]+px' "${targets[@]}" 2>/dev/null \
    | grep -v -e 'src/theme/tokens.css' -e 'ui-lint: allow' \
    | sed 's/^/px-size: /' || true)

# Denylist of icon-shaped characters. Not a Unicode range, so plain -F
# (fixed-string) matching is enough — no PCRE required.
GLYPH_DENY='× ✕ ✗ ＋ ✚ ⎇ ✎ ▾ ▴ ● ○ ◐ ◆ ■ ▶ ⟳ ↻'
glyph_args=()
for ch in $GLYPH_DENY; do
  glyph_args+=(-e "$ch")
done
while IFS= read -r line; do report "$line"; done < <(
  grep -rnF --include='*.ts' --include='*.html' --include='*.css' \
    "${glyph_args[@]}" "${glyph_targets[@]}" 2>/dev/null \
    | grep -v -e 'ui-lint: allow' \
    | sed 's/^/glyph: /' || true)

echo "ui-lint: $n violation(s)"
[[ $strict -eq 1 && $n -gt 0 ]] && exit 1
exit 0
