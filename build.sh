#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

echo "Building hive..."
go build -o hive .

echo "Installing hive to /usr/local/bin..."
cp hive /usr/local/bin/hive

echo "Done. Run 'hive' to start."
