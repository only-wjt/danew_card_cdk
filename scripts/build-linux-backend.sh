#!/usr/bin/env bash
# mattn/go-sqlite3 requires CGO. A CGO-disabled ELF starts but cannot open the DB.
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT="${1:?output binary path required}"
VER="${2:-$(tr -d ' \n' < "$ROOT/VERSION")}"
if [[ "$(uname -s)" != Linux || "$(uname -m)" != x86_64 ]]; then
  echo "Build on Linux amd64 (or in the repository Docker image); SQLite requires a Linux C compiler." >&2
  exit 1
fi
command -v gcc >/dev/null || { echo "gcc and libc development files are required" >&2; exit 1; }
mkdir -p "$(dirname "$OUT")"
cd "$ROOT/backend"
CGO_ENABLED=1 GOOS=linux GOARCH=amd64 CC=gcc \
  go build -trimpath -tags 'netgo osusergo sqlite_omit_load_extension' \
  -ldflags="-s -w -linkmode external -extldflags '-static' -X github.com/danew/cdk-recharge-system/internal/handler.BuildVersion=${VER}" \
  -o "$OUT" ./cmd/server
