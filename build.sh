#!/usr/bin/env bash
# build.sh — Build the Hive desktop app.
#
# Default target: macOS universal .app at cmd/hivegui/build/bin/hivegui.app
# (with hived bundled inside Contents/MacOS/).
#
# Optional flags:
#   --platform <macos|windows|all>   target (default: macos)
#   --zip                            also write release/Hive-<version>-<plat>.zip
#   --version <tag>                  version string for the zip name (default: dev)
#   --open                           open the macOS .app on success
#
# Requires: go (1.22+), node (18+),
#           wails (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`),
#           lipo (Xcode command line tools, macOS target only).
set -euo pipefail

cd "$(dirname "$0")"

zip_artifact=0
open_after=0
version="dev"
platform="macos"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --zip)      zip_artifact=1; shift ;;
    --version)  version="$2"; shift 2 ;;
    --open)     open_after=1; shift ;;
    --platform) platform="$2"; shift 2 ;;
    -h|--help)
      sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

case "$platform" in
  macos|windows|all) ;;
  *) echo "error: --platform must be one of: macos, windows, all" >&2; exit 2 ;;
esac

# Reject versions with characters that would break the -ldflags
# string we splice this into. Real release tags are dotted digits
# with optional pre-release suffixes (e.g. "0.4.1", "1.0.0-rc1") so
# this is conservative.
if [[ ! "$version" =~ ^[A-Za-z0-9._+-]+$ ]]; then
  echo "error: --version contains unsupported characters: $version" >&2
  exit 2
fi

if ! command -v wails >/dev/null 2>&1; then
  if [[ -x "$(go env GOPATH)/bin/wails" ]]; then
    export PATH="$(go env GOPATH)/bin:$PATH"
  else
    echo "error: wails CLI not found. Install with:" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2
    exit 1
  fi
fi

# Frontend deps. Wails skips `npm install` when frontend/package.json
# matches a cached MD5, which can leave node_modules stale after a
# pull that adds a dep. Force a clean install when package.json is
# newer than node_modules.
echo "==> Installing frontend dependencies"
if [[ ! -d cmd/hivegui/frontend/node_modules ]] \
   || [[ cmd/hivegui/frontend/package.json -nt cmd/hivegui/frontend/node_modules ]]; then
  ( cd cmd/hivegui/frontend && npm install --no-audit --no-fund )
else
  echo "  (up to date)"
fi

# Compute build identity stamped into both binaries via -ldflags. The
# GUI compares its own BuildID to the daemon's at handshake time and
# surfaces a banner if they differ — see internal/buildinfo.
build_id="$(git rev-parse --short HEAD 2>/dev/null || echo unknown)"
# Use porcelain so untracked files (which `git diff` ignores) also
# trigger the -dirty marker.
if [[ -n "$(git status --porcelain 2>/dev/null)" ]]; then
  build_id="${build_id}-dirty"
fi
buildinfo_pkg="github.com/lucascaro/hive/internal/buildinfo"
# Stamp both the commit-hash BuildID (always) and the release version
# (only when --version was passed; otherwise the linker leaves it
# empty and Version() returns "dev").
ldflags_id="-X ${buildinfo_pkg}.buildIDOverride=${build_id}"
if [[ "$version" != "dev" ]]; then
  ldflags_id="${ldflags_id} -X ${buildinfo_pkg}.versionOverride=${version}"
fi
echo "==> BuildID: ${build_id}  Version: ${version}"

build_macos() {
  if ! command -v lipo >/dev/null 2>&1; then
    echo "error: lipo not found (install Xcode command-line tools: xcode-select --install)" >&2
    exit 1
  fi

  echo "==> [macos] Building Wails universal .app"
  ( cd cmd/hivegui && wails build -platform darwin/universal -clean \
      -ldflags "${ldflags_id}" )

  echo "==> [macos] Building hived (universal)"
  mkdir -p .build
  GOOS=darwin GOARCH=amd64 go build -trimpath \
    -ldflags="-s -w ${ldflags_id}" \
    -o .build/hived-darwin-amd64 ./cmd/hived
  GOOS=darwin GOARCH=arm64 go build -trimpath \
    -ldflags="-s -w ${ldflags_id}" \
    -o .build/hived-darwin-arm64 ./cmd/hived
  lipo -create -output cmd/hivegui/build/bin/hivegui.app/Contents/MacOS/hived \
    .build/hived-darwin-amd64 .build/hived-darwin-arm64
  rm -rf .build

  APP=cmd/hivegui/build/bin/hivegui.app
  echo "==> [macos] Built $APP"
  file "$APP/Contents/MacOS/hivegui" | head -1
  file "$APP/Contents/MacOS/hived"   | head -1

  if [[ $zip_artifact -eq 1 ]]; then
    mkdir -p release
    out="release/Hive-${version}-macos-universal.zip"
    rm -f "$out"
    ( cd cmd/hivegui/build/bin && zip -rq "../../../../$out" hivegui.app )
    echo "==> [macos] Packaged $out ($(du -h "$out" | cut -f1))"
  fi

  if [[ $open_after -eq 1 ]]; then
    open "$APP"
  fi
}

build_windows() {
  echo "==> [windows] Building Wails hivegui.exe (amd64)"
  ( cd cmd/hivegui && wails build -platform windows/amd64 -clean \
      -ldflags "${ldflags_id}" )

  echo "==> [windows] Building hived.exe (amd64)"
  mkdir -p .build
  GOOS=windows GOARCH=amd64 go build -trimpath \
    -ldflags="-s -w ${ldflags_id}" \
    -o .build/hived.exe ./cmd/hived

  BIN=cmd/hivegui/build/bin
  cp .build/hived.exe "$BIN/hived.exe"
  rm -rf .build
  echo "==> [windows] Built $BIN/hivegui.exe + $BIN/hived.exe"

  if [[ $zip_artifact -eq 1 ]]; then
    mkdir -p release
    out="release/Hive-${version}-windows-amd64.zip"
    rm -f "$out"
    ( cd "$BIN" && zip -q "../../../../$out" hivegui.exe hived.exe )
    echo "==> [windows] Packaged $out ($(du -h "$out" | cut -f1))"
  fi
}

# Order matters: macOS uses `wails build -clean` which wipes
# cmd/hivegui/build/bin, so build the OTHER platform's outputs after
# macOS if both are requested. (Wails Windows build also -cleans, but
# the macOS .app zip is already written by then.)
if [[ "$platform" == "macos" || "$platform" == "all" ]]; then
  build_macos
fi
if [[ "$platform" == "windows" || "$platform" == "all" ]]; then
  build_windows
fi
