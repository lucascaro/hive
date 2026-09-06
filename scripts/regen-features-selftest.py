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
    check(
        "site/features.json is a declared release target",
        "site/features.json" in regen.RELEASE_TARGETS,
        True,
    )
    release_sh = (HERE / "release.sh").read_text(encoding="utf-8")
    check(
        "release.sh stages generated files via --list-targets",
        "--list-targets" in release_sh,
        True,
    )

    if failures:
        for f in failures:
            sys.stderr.write(f"regen-features-selftest: FAIL {f}\n")
        return 1
    sys.stderr.write("regen-features-selftest: OK\n")
    return 0


if __name__ == "__main__":
    sys.exit(main())
