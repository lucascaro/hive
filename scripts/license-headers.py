#!/usr/bin/env python3
# SPDX-License-Identifier: GPL-3.0-only
# Copyright (C) 2026 Lucas Caro
# See LICENSE and ADDITIONAL-TERMS.md.
"""Add (or verify with --check) the SPDX licence header on first-party sources.

  scripts/license-headers.py           # rewrite files in place
  scripts/license-headers.py --check   # exit 1 listing files that are wrong

Idempotent: an existing SPDX/Copyright header (e.g. the old MIT one) is
replaced, never stacked. Covers every git-tracked Go, TS/TSX, JS/MJS/CJS,
Objective-C, CSS and shell file except generated or vendored code.
"""
import re
import subprocess
import sys

SPDX = "GPL-3.0-only"
COPYRIGHT = "Copyright (C) 2026 Lucas Caro"
LINES = [
    f"SPDX-License-Identifier: {SPDX}",
    COPYRIGHT,
    "See LICENSE and ADDITIONAL-TERMS.md.",
]
SLASH = {".go", ".ts", ".tsx", ".js", ".mjs", ".cjs", ".m"}
HASH = {".sh"}
BLOCK = {".css"}
SKIP = re.compile(
    r"(^|/)(node_modules|dist|vendor|wailsjs|testdata|generated)/"
    r"|\.min\.|(^|/)\.claude/"
)
GENERATED = re.compile(r"^(//|#|/\*) ?(Code generated|@generated|DO NOT EDIT)", re.M)


def header(ext):
    if ext in SLASH:
        return "".join(f"// {l}\n" for l in LINES)
    if ext in HASH:
        return "".join(f"# {l}\n" for l in LINES)
    return "/*\n" + "".join(f" * {l}\n" for l in LINES) + " */\n"


def old_header_len(text, ext):
    """Length of an existing licence header at the top, or 0."""
    if ext in BLOCK:
        m = re.match(r"/\*(?:(?!\*/).)*?(SPDX-License-Identifier|Copyright|MIT License).*?\*/\n\n?", text, re.S)
        return m.end() if m else 0
    pre = "//" if ext in SLASH else "#"
    lines = text.split("\n")
    n = 0
    while n < len(lines) and lines[n].startswith(pre):
        n += 1
    block = "\n".join(lines[:n])
    if n and re.search(r"SPDX-License-Identifier|Copyright \(c\)|Copyright \(C\)|MIT License", block):
        # Only consume the comment block when it is a licence header, and
        # the blank line that separated it from the code.
        end = len("\n".join(lines[:n])) + 1
        while text[end:end + 1] == "\n":
            end += 1
        return end
    return 0


def rewrite(path, text):
    ext = path[path.rindex("."):]
    if GENERATED.search(text[:600]):
        return text
    shebang = ""
    if text.startswith("#!"):
        nl = text.index("\n") + 1
        shebang, text = text[:nl], text[nl:]
        if ext in HASH:
            shebang += ""  # header follows the shebang line directly
    text = text[old_header_len(text, ext):] if old_header_len(text, ext) else text.lstrip("\n")
    return shebang + header(ext) + ("\n" if text else "") + text


def main():
    check = "--check" in sys.argv
    files = subprocess.check_output(["git", "ls-files"], text=True).splitlines()
    bad = []
    for f in files:
        if "." not in f.rsplit("/", 1)[-1]:
            continue
        ext = f[f.rindex("."):]
        if ext not in SLASH | HASH | BLOCK or SKIP.search(f):
            continue
        with open(f, encoding="utf-8") as fh:
            text = fh.read()
        new = rewrite(f, text)
        if new != text:
            bad.append(f)
            if not check:
                with open(f, "w", encoding="utf-8") as fh:
                    fh.write(new)
    verb = "missing/wrong header" if check else "updated"
    print(f"{len(bad)} files {verb}")
    if check and bad:
        print("\n".join(bad))
        sys.exit(1)


main()
