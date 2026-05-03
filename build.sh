#!/usr/bin/env bash
# build.sh — Build the Hive macOS .app (universal: arm64 + x86_64).
#
# Output: cmd/hivegui/build/bin/hivegui.app
#         (with hived bundled inside Contents/MacOS/)
#
# Optional flags:
#   --zip              also write release/Hive-<version>-macos-universal.zip
#   --version <tag>    version string for the zip name (default: dev)
#   --open             open the .app on success
#
# Requires: go (1.22+), node (18+), wails (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`),
#           lipo (Xcode command line tools).
set -euo pipefail

cd "$(dirname "$0")"

zip_artifact=0
open_after=0
version="dev"

while [[ $# -gt 0 ]]; do
  case "$1" in
    --zip)     zip_artifact=1; shift ;;
    --version) version="$2"; shift 2 ;;
    --open)    open_after=1; shift ;;
    -h|--help)
      sed -n '2,15p' "$0" | sed 's/^# \{0,1\}//'
      exit 0 ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

if ! command -v wails >/dev/null 2>&1; then
  if [[ -x "$(go env GOPATH)/bin/wails" ]]; then
    export PATH="$(go env GOPATH)/bin:$PATH"
  else
    echo "error: wails CLI not found. Install with:" >&2
    echo "  go install github.com/wailsapp/wails/v2/cmd/wails@latest" >&2
    exit 1
  fi
fi

if ! command -v lipo >/dev/null 2>&1; then
  echo "error: lipo not found (install Xcode command-line tools: xcode-select --install)" >&2
  exit 1
fi

# 1. Frontend deps. Wails skips `npm install` when frontend/package.json
#    matches a cached MD5, which can leave node_modules stale after a
#    pull that adds a dep. Force a clean install when package.json is
#    newer than node_modules.
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
if ! git diff --quiet 2>/dev/null || ! git diff --cached --quiet 2>/dev/null; then
  build_id="${build_id}-dirty"
fi
buildinfo_pkg="github.com/lucascaro/hive/internal/buildinfo"
echo "==> BuildID: ${build_id}"

# 2. Wails universal app bundle (frontend + GUI binary).
echo "==> Building Wails universal .app"
( cd cmd/hivegui && wails build -platform darwin/universal -clean \
    -ldflags "-X ${buildinfo_pkg}.BuildID=${build_id}" )

# 3. hived universal binary, lipo'd into the .app.
echo "==> Building hived (universal)"
mkdir -p .build
GOOS=darwin GOARCH=amd64 go build -trimpath \
  -ldflags="-s -w -X ${buildinfo_pkg}.BuildID=${build_id}" \
  -o .build/hived-darwin-amd64 ./cmd/hived
GOOS=darwin GOARCH=arm64 go build -trimpath \
  -ldflags="-s -w -X ${buildinfo_pkg}.BuildID=${build_id}" \
  -o .build/hived-darwin-arm64 ./cmd/hived
lipo -create -output cmd/hivegui/build/bin/hivegui.app/Contents/MacOS/hived \
  .build/hived-darwin-amd64 .build/hived-darwin-arm64
rm -rf .build

APP=cmd/hivegui/build/bin/hivegui.app
echo "==> Built $APP"
file "$APP/Contents/MacOS/hivegui" | head -1
file "$APP/Contents/MacOS/hived"   | head -1

if [[ $zip_artifact -eq 1 ]]; then
  mkdir -p release
  out="release/Hive-${version}-macos-universal.zip"
  rm -f "$out"
  ( cd cmd/hivegui/build/bin && zip -rq "../../../../$out" hivegui.app )
  echo "==> Packaged $out ($(du -h "$out" | cut -f1))"
fi

if [[ $open_after -eq 1 ]]; then
  open "$APP"
fi
