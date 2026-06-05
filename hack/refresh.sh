#!/bin/sh
#
# Refresh the Dockerized local dev stack: build the slop image and stop the
# previous stack in parallel, then start it again.
#
# Optional chat API config for local dev:
#
#   SLOP_CHAT_BASE_URL=https://your-openai-compatible-host/v1 \
#   SLOP_CHAT_API_KEY=your-api-key \
#   SLOP_CHAT_MODEL=your-model \
#   ./hack/refresh.sh
#
# Or place the same values in an uncommitted .env file; Docker Compose reads it
# automatically for compose.dev.yaml.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)

cd "$ROOT"

build_status=0
down_status=0

docker compose -f compose.dev.yaml build slop &
build_pid=$!

docker compose -f compose.dev.yaml down &
down_pid=$!

wait "$build_pid" || build_status=$?
wait "$down_pid" || down_status=$?

if [ "$build_status" -ne 0 ]; then
  printf 'refresh failed: image build exited with %s\n' "$build_status" >&2
  exit "$build_status"
fi

if [ "$down_status" -ne 0 ]; then
  printf 'refresh failed: container shutdown exited with %s\n' "$down_status" >&2
  exit "$down_status"
fi

exec docker compose -f compose.dev.yaml up --remove-orphans
