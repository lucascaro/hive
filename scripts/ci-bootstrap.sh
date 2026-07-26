#!/usr/bin/env bash
# ci-bootstrap.sh — shared Wails bootstrap for CI and build.sh.
#
# Single source of truth for the pinned Wails CLI version. Seeds an
# empty frontend/dist (the //go:embed all:frontend/dist directive in
# cmd/hivegui/main.go needs something to read before the real vite
# build overwrites it), installs the Wails CLI, and generates the
# wailsjs/ bindings that main.js imports.
set -euo pipefail
cd "$(dirname "$0")/.."

WAILS_VERSION=v2.12.0

mkdir -p cmd/hivegui/frontend/dist
echo placeholder > cmd/hivegui/frontend/dist/.placeholder

go install "github.com/wailsapp/wails/v2/cmd/wails@${WAILS_VERSION}"

export PATH="$(go env GOPATH)/bin:$PATH"
(cd cmd/hivegui && wails generate module)
