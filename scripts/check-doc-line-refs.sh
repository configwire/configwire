#!/usr/bin/env bash
# Docs must cite file + symbol, never a source line number: line refs go stale.
set -euo pipefail
cd "$(dirname "$0")/.."

if grep -RInE '[A-Za-z0-9_./-]+\.go:[0-9]+' docs README.md; then
  echo "doc line refs are banned; keep file + symbol" >&2
  exit 1
fi
