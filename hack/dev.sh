#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DB_PATH=${SLOPR_DB_PATH:-/tmp/slopr-dev.db}

cleanup() {
  if [ -n "${BACKEND_PID:-}" ]; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

(
  cd "$ROOT/backend"
  SLOPR_SESSION_SECRET=${SLOPR_SESSION_SECRET:-dev-secret} \
  SLOPR_AUTH_MODE=dev \
  SLOPR_ADDR=127.0.0.1:8080 \
  SLOPR_PUBLIC_URL=http://127.0.0.1:8080 \
  SLOPR_DB_PATH="$DB_PATH" \
  go run ./cmd/slopr
) &
BACKEND_PID=$!

cd "$ROOT/ui"
npm run dev -- --host 127.0.0.1
