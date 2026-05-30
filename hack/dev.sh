#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
DB_PATH=${SPARK_DB_PATH:-/tmp/spark-dev.db}

cleanup() {
  if [ -n "${BACKEND_PID:-}" ]; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
}
trap cleanup EXIT INT TERM

(
  cd "$ROOT/backend"
  SPARK_SESSION_SECRET=${SPARK_SESSION_SECRET:-dev-secret} \
  SPARK_AUTH_MODE=dev \
  SPARK_ADDR=127.0.0.1:8080 \
  SPARK_PUBLIC_URL=http://127.0.0.1:8080 \
  SPARK_DB_PATH="$DB_PATH" \
  go run ./cmd/spark
) &
BACKEND_PID=$!

cd "$ROOT/frontend"
npm run dev -- --host 127.0.0.1
