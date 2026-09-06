#!/usr/bin/env python3
"""Self-test for regen-generated.py's stamp_features_release().

That function rewrites site/features.json — the one user-facing feature list
the website and the app's What's New modal both read — and it only ever runs
on the release path, where a mistake is discovered by users rather than by CI.
So it gets its own evidence that it still fires, the same way
check-daemon-contract-selftest.sh covers the contract gate.

Run: scripts/regen-features-selftest.py
"""
from __future__ import annotations

import importlib.util
import json
import sys
import tempfile
from pathlib import Path

HERE = Path(__file__).resolve().parent


def load_regen():
    """Import regen-generated.py despite the hyphen in its filename."""
    spec = importlib.util.spec_from_file_location(
        "regen_generated", HERE / "regen-generated.py"
    )
    assert spec and spec.loader
    mod = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(mod)
    return mod


def main() -> int:
    regen = load_regen()
    failures: list[str] = []

    def check(label: str, got, want) -> None:
        if got != want:
            failures.append(f"{label}: got {got!r}, want {want!r}")

    with tempfile.TemporaryDirectory() as tmp:
        features_path = Path(tmp) / "features.json"
        regen.FEATURES = features_path

        # An Unreleased entry is stamped; a released one and a planned one
        # are left exactly as they were.
        features_path.write_text(
            json.dumps(
                [
                    {"title": "new", "status": "shipped", "since": "Unreleased"},
                    {"title": "old", "status": "shipped", "since": "2.6.0"},
                    {"title": "later", "status": "planned"},
                ],
                indent=2,
            )
            + "\n",
            encoding="utf-8",
        )
        check("first run reports a change", regen.stamp_features_release("2.7.0"), True)
        out = json.loads(features_path.read_text(encoding="utf-8"))
        check("Unreleased is stamped", out[0]["since"], "2.7.0")
        check("a released entry is untouched", out[1]["since"], "2.6.0")
        check("a planned entry gains no since", "since" in out[2], False)
        check("entry order is preserved", [f["title"] for f in out], ["new", "old", "later"])

        # Idempotent: a second release with nothing left to stamp is a no-op,
        # so `--check` can never report drift for it.
        check("second run is a no-op", regen.stamp_features_release("2.8.0"), False)
        again = json.loads(features_path.read_text(encoding="utf-8"))
        check("a no-op run rewrites nothing", again[0]["since"], "2.7.0")

        # Non-ASCII survives the round trip: the real list is full of ⌘ and —.
        features_path.write_text(
            json.dumps(
                [{"title": "⌘Z — undo", "status": "shipped", "since": "Unreleased"}],
                indent=2,
                ensure_ascii=False,
            )
            + "\n",
            encoding="utf-8",
        )
        regen.stamp_features_release("2.7.0")
        raw = features_path.read_text(encoding="utf-8")
        check("non-ASCII is not escaped", "⌘Z — undo" in raw, True)

        # A missing file is not an error — a checkout without site/ still releases.
        features_path.unlink()
        check("missing file is a no-op", regen.stamp_features_release("2.7.0"), False)

    # The stamp is only useful if the release commit carries it. release.sh
    # must discover the file list from --list-targets rather than repeating
    # one, because a hand-maintained copy is precisely what let the
    # features.json stamp go uncommitted the first time.
    release_sh = (HERE / "release.sh").read_text(encoding="utf-8")
    check(
        "release.sh stages generated files via --list-targets",
        "--list-targets" in release_sh,
        True,
    )

    # Drive the CLI, not the constant. Asserting `"site/features.json" in
    # RELEASE_TARGETS` would pass even if main()'s targets table stopped
    # calling stamp_features_release — RELEASE_TARGETS and that table are two
    # hand-maintained lists, and this file exists to catch exactly that class
    # of drift, not to re-introduce it one layer up.
    import contextlib
    import io

    buf = io.StringIO()
    with contextlib.redirect_stdout(buf):
        rc = regen.main(["--list-targets"])
    check("--list-targets exits 0", rc, 0)
    check(
        "--list-targets prints RELEASE_TARGETS verbatim",
        buf.getvalue().split(),
        regen.RELEASE_TARGETS,
    )

    # End to end: a real `--release` run must actually stamp the file. This is
    # the assertion that fails if the targets table stops wiring the stamp in.
    with tempfile.TemporaryDirectory() as tmp:
        features_path = Path(tmp) / "features.json"
        features_path.write_text(
            json.dumps([{"title": "new", "status": "shipped", "since": "Unreleased"}])
            + "\n",
            encoding="utf-8",
        )
        regen.FEATURES = features_path
        # Neutralise the sibling aggregates: this run is about the feature
        # list, and CHANGELOG promotion needs a real tree.
        regen.regen_changelog = lambda *_a, **_k: False
        regen.regen_specs_index = lambda: False
        regen.regen_tech_debt = lambda: False
        rc = regen.main(["--release", "2.7.0"])
        check("--release exits 0", rc, 0)
        stamped = json.loads(features_path.read_text(encoding="utf-8"))
        check("--release stamps the feature list", stamped[0]["since"], "2.7.0")

    if failures:
        for f in failures:
            sys.stderr.write(f"regen-features-selftest: FAIL {f}\n")
        return 1
    sys.stderr.write("regen-features-selftest: OK\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
