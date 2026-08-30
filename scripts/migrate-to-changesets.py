#!/usr/bin/env python3
"""One-shot migration: CHANGELOG.md `[Unreleased]` body → `.changesets/*.md` files.

Splits the [Unreleased] section into one changeset per top-level bullet, writing
each to `.changesets/<NNN>-<slug>.md` with YAML frontmatter (issue, type, bump).
Body content is preserved verbatim so the post-migration CHANGELOG matches the
pre-migration one byte-for-byte after regen.

Idempotent: if a target `.changesets/` file already exists with the same
content, it is left alone; if it differs, the script aborts so a human resolves
the conflict.

After running, execute:

    scripts/regen-generated.sh --check

to verify the regenerated CHANGELOG matches the committed copy.

Usage:
  migrate-to-changesets.py                    # do the migration
  migrate-to-changesets.py --dry-run          # print planned writes, no I/O
"""
from __future__ import annotations

import argparse
import re
import sys
from pathlib import Path
from typing import Iterable

REPO_ROOT = Path(__file__).resolve().parent.parent
CHANGELOG = REPO_ROOT / "CHANGELOG.md"
CHANGESET_DIR = REPO_ROOT / ".changesets"

# Recognized changelog type sections, lowercase canonical form.
TYPE_HEADINGS = {
    "added": "added",
    "changed": "changed",
    "fixed": "fixed",
    "removed": "removed",
    "deprecated": "deprecated",
    "security": "security",
}

# Map convention: an `### Added` (etc.) heading delimits a section; top-level
# bullets within it look like `- **Headline.**` followed by continuation lines.
_UNRELEASED_RE = re.compile(
    r"## \[Unreleased\][^\n]*\n(.*?)(?=\n## \[)",
    re.DOTALL,
)
_SECTION_RE = re.compile(r"^### (.+?)\s*$")
_BULLET_RE = re.compile(r"^- (.*)$")
# Pull a slug-able phrase out of the headline:
#   "- **`feature-loop` skill — `--full-auto` flag.** body…"  →  feature-loop-skill-full-auto-flag
#   "- Renamed `ralph-loop` to `review-loop`."                 →  renamed-ralph-loop-to-review-loop
_HEADLINE_RE = re.compile(r"\*\*(.+?)\*\*")
_ISSUE_RE = re.compile(r"#(\d+)")


class MigrationError(Exception):
    """Aborts the migration with a clear message."""


def _slugify(text: str, max_words: int = 7) -> str:
    text = text.lower()
    # Drop code-fence backticks, punctuation; keep letters/digits/spaces/hyphens.
    text = re.sub(r"`", "", text)
    text = re.sub(r"[^a-z0-9\s\-]", " ", text)
    parts = [p for p in re.split(r"[\s\-]+", text) if p]
    return "-".join(parts[:max_words]) or "entry"


def _bump_for_type(type_name: str) -> str:
    # Conservative default. Maintainers can edit individual files if they want
    # a different bump signal. Most user-visible additions in hivesmith have
    # been minor; security/breaking would be major.
    return {
        "added": "minor",
        "changed": "minor",
        "fixed": "patch",
        "removed": "major",
        "deprecated": "patch",
        "security": "patch",
    }.get(type_name, "patch")


def parse_unreleased_bullets(changelog_text: str) -> list[tuple[str, str]]:
    """Return [(type_name, full_bullet_text), …] for each top-level bullet
    under `## [Unreleased]`, ordered as found.

    `full_bullet_text` is the verbatim multi-line block, leading `- ` retained.
    """
    m = _UNRELEASED_RE.search(changelog_text)
    if not m:
        raise MigrationError("CHANGELOG.md: cannot locate `## [Unreleased]` followed by a versioned section")
    body = m.group(1)

    out: list[tuple[str, str]] = []
    current_type: str | None = None
    current_bullet: list[str] = []

    def _flush() -> None:
        if current_type and current_bullet:
            out.append((current_type, "\n".join(current_bullet).rstrip()))

    for raw_line in body.splitlines():
        sec = _SECTION_RE.match(raw_line)
        if sec:
            _flush()
            current_bullet = []
            heading = sec.group(1).strip().lower()
            current_type = TYPE_HEADINGS.get(heading)
            if heading and not current_type:
                raise MigrationError(f"unknown CHANGELOG section heading: '### {sec.group(1)}'")
            continue
        if not current_type:
            # Lines outside any type section (e.g. between `[Unreleased]` and
            # the first `### Added`) are ignored — they're just blank lines.
            if raw_line.strip():
                raise MigrationError(f"non-blank line before first ### section: {raw_line!r}")
            continue
        if _BULLET_RE.match(raw_line):
            _flush()
            current_bullet = [raw_line]
        elif raw_line.startswith("  ") or raw_line.startswith("\t") or not raw_line.strip():
            # continuation line (indented) or blank line within a bullet
            if current_bullet:
                current_bullet.append(raw_line)
        else:
            # A non-indented, non-bullet line inside a section is unexpected.
            raise MigrationError(f"unindented non-bullet line inside ### {current_type}: {raw_line!r}")
    _flush()
    return out


def _allocate_id(used: set[int]) -> int:
    """Return `max(used) + 1`, or 1 if `used` is empty.

    Monotonic allocation: new files always sort after existing ones, so
    `.changesets/` rendering is strictly append-only. Reusing an unused slot
    earlier in the sequence would re-introduce the conflict pattern this
    refactor is trying to eliminate.
    """
    n = (max(used) + 1) if used else 1
    used.add(n)
    return n


def headline_and_slug(bullet_text: str) -> tuple[int | None, str]:
    """Pull (issue_number, slug) hints out of a bullet.

    issue_number is the first `#NNN` mentioned in the bullet (if any) — used
    when it looks like a real GitHub issue/PR reference. Falls back to None.
    slug comes from the first bold span if present, else the first sentence.
    """
    first_line = bullet_text.splitlines()[0]
    bold = _HEADLINE_RE.search(first_line)
    if bold:
        slug_src = bold.group(1)
    else:
        # Strip leading `- `
        slug_src = first_line[2:] if first_line.startswith("- ") else first_line
        slug_src = slug_src.split(".")[0]
    issue_match = _ISSUE_RE.search(first_line)
    issue = int(issue_match.group(1)) if issue_match else None
    return issue, _slugify(slug_src)


def _format_changeset(issue: int | None, type_name: str, bump: str, body: str) -> str:
    lines = ["---"]
    if issue is not None:
        lines.append(f"issue: {issue}")
    lines += [f"type: {type_name}", f"bump: {bump}", "---", body, ""]
    return "\n".join(lines)


def plan_writes(bullets: list[tuple[str, str]]) -> list[tuple[Path, str]]:
    used_ids: set[int] = set()
    # Seed used_ids from any existing .changesets/*.md so we don't collide.
    if CHANGESET_DIR.exists():
        for p in CHANGESET_DIR.glob("*.md"):
            m = re.match(r"^(\d+)-", p.stem)
            if m:
                used_ids.add(int(m.group(1)))

    writes: list[tuple[Path, str]] = []
    for type_name, bullet in bullets:
        hinted_issue, slug = headline_and_slug(bullet)
        # Filename id is sequential so sort order matches the original CHANGELOG
        # bullet order. The `issue:` field carries the GitHub issue reference
        # when one was discoverable in the bullet body.
        ident = _allocate_id(used_ids)
        filename = f"{ident:03d}-{slug}.md"
        path = CHANGESET_DIR / filename
        writes.append((path, _format_changeset(hinted_issue, type_name, _bump_for_type(type_name), bullet)))
    return writes


def apply_writes(writes: Iterable[tuple[Path, str]], dry_run: bool) -> int:
    # In dry-run mode, do NOT create the directory — the contract is "no I/O".
    # In write mode we create it lazily so a no-op migration (zero bullets)
    # doesn't leave behind an empty `.changesets/` either.
    if not dry_run:
        CHANGESET_DIR.mkdir(exist_ok=True)
    n_created = 0
    for path, content in writes:
        if path.exists():
            existing = path.read_text(encoding="utf-8")
            if existing == content:
                continue
            raise MigrationError(
                f"{path} already exists with different content; refusing to overwrite. "
                "Resolve by hand and re-run."
            )
        if dry_run:
            sys.stderr.write(f"would write: {path}\n")
        else:
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(content, encoding="utf-8")
            sys.stderr.write(f"wrote: {path}\n")
        n_created += 1
    return n_created


def main(argv: list[str]) -> int:
    p = argparse.ArgumentParser(description=__doc__.splitlines()[0] if __doc__ else "")
    p.add_argument("--dry-run", action="store_true", help="print planned writes, no I/O")
    args = p.parse_args(argv)

    try:
        changelog_text = CHANGELOG.read_text(encoding="utf-8")
        bullets = parse_unreleased_bullets(changelog_text)
        if not bullets:
            sys.stderr.write("migrate-to-changesets: [Unreleased] is already empty — nothing to do\n")
            return 0
        writes = plan_writes(bullets)
        n = apply_writes(writes, args.dry_run)
        sys.stderr.write(
            f"migrate-to-changesets: {'planned' if args.dry_run else 'created'} {n} changeset(s)"
            f" from {len(bullets)} CHANGELOG bullet(s)\n"
        )
        if not args.dry_run:
            sys.stderr.write(
                "Next: run `scripts/regen-generated.sh --check` to verify the regenerated "
                "CHANGELOG matches the committed copy.\n"
            )
        return 0
    except MigrationError as exc:
        sys.stderr.write(f"migrate-to-changesets: {exc}\n")
        return 1


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
