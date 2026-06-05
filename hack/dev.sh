#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DB_PATH=${SLOP_DB_PATH:-/tmp/slop-dev.db}

cleanup() {
  if [ -n "${BACKEND_PID:-}" ]; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

(
  cd "$ROOT/backend"
  SLOP_SESSION_SECRET=${SLOP_SESSION_SECRET:-dev-secret} \
  SLOP_AUTH_MODE=dev \
  SLOP_ADDR=127.0.0.1:8080 \
  SLOP_PUBLIC_URL=http://127.0.0.1:8080 \
  SLOP_DB_PATH="$DB_PATH" \
  go run ./cmd/slop
) &
BACKEND_PID=$!

cd "$ROOT/frontend"
npm run dev -- --host 127.0.0.1
