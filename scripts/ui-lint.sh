#!/usr/bin/env bash
# UI literal lint. Rules (docs/design-docs/ui/README.md, principle 3):
#   hex      — raw #rgb/#rrggbb/#rrggbbaa outside src/theme/{tokens,themes}.css
#   px-size  — font-size: <n>px outside src/theme/tokens.css
#   glyph    — non-ASCII characters in src/app/**/*.ts and index.html
#              (icons come from the sprite; text separators are allow-listed)
# A trailing `/* ui-lint: allow */` (CSS) or `// ui-lint: allow` (TS) exempts a line.
# Exit 0 in warn mode; --strict exits 1 on any violation. Phase 2 flips CI to --strict.
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
# The glyph rule needs PCRE (`grep -P`) for a Unicode range match. GNU grep
# (Linux/CI) and ugrep support -P; stock BSD grep on macOS does not. Rather
# than hard-fail (set -e would kill the script) or silently report zero
# glyph violations (worse — a lint that quietly stops checking), we probe
# once and skip the glyph rule with a clearly printed warning when -P is
# unavailable.
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
# Glyph rule defaults to src/app + index.html (its historical scope) when no
# explicit targets are given, but honours explicit targets like the other
# two rules do — otherwise `ui-lint.sh --strict some/fixture.ts` would
# silently ignore the argument and scan the whole app tree instead.
glyph_targets=("${targets[@]}")
if [[ $custom -eq 0 ]]; then
  glyph_targets=("$FE/src/app" "$FE/index.html")
fi

n=0
report() { echo "$1"; n=$((n + 1)); }

while IFS= read -r line; do report "$line"; done < <(
  grep -rnE --include='*.css' '#[0-9a-fA-F]{3,8}\b' "${targets[@]}" 2>/dev/null \
    | grep -v -e 'src/theme/tokens.css' -e 'src/theme/themes.css' -e 'ui-lint: allow' \
    | sed 's/^/hex: /' || true)

while IFS= read -r line; do report "$line"; done < <(
  grep -rnE --include='*.css' 'font-size:\s*[0-9.]+px' "${targets[@]}" 2>/dev/null \
    | grep -v -e 'src/theme/tokens.css' -e 'ui-lint: allow' \
    | sed 's/^/px-size: /' || true)

has_pcre=1
echo x | grep -qP x 2>/dev/null || has_pcre=0
if [[ $has_pcre -eq 1 ]]; then
  ALLOW='…·⌘⇧⌥⌃←→↑↓'
  while IFS= read -r line; do report "$line"; done < <(
    grep -rnP --include='*.ts' --include='*.html' "[^\x00-\x7F$ALLOW]" "${glyph_targets[@]}" 2>/dev/null \
      | grep -v -e 'ui-lint: allow' -e '^\S*:\s*//' \
      | sed 's/^/glyph: /' || true)
else
  echo "ui-lint: WARNING: grep -P (PCRE) not supported by this grep — glyph rule SKIPPED. Install GNU grep or ugrep, or run on CI (ubuntu-latest), to get glyph coverage." >&2
fi

echo "ui-lint: $n violation(s)"
[[ $strict -eq 1 && $n -gt 0 ]] && exit 1
exit 0
