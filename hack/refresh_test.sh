#!/bin/sh
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
TMP_DIR=$(mktemp -d "${TMPDIR:-/tmp}/loom-refresh-test.XXXXXX")

cleanup() {
  rm -rf "$TMP_DIR"
}
trap cleanup EXIT INT TERM

mkdir -p "$TMP_DIR/bin"

cat > "$TMP_DIR/bin/docker" <<'STUB'
#!/bin/sh
set -eu

LOG_FILE=${BACKEND_REFRESH_TEST_LOG:?}

printf '%s\n' "$*" >> "$LOG_FILE"

case "$*" in
  "compose -f compose.dev.yaml build loom")
    printf '%s\n' build-start >> "$LOG_FILE"
    sleep 1
    printf '%s\n' build-done >> "$LOG_FILE"
    ;;
  "compose -f compose.dev.yaml down")
    printf '%s\n' down-start >> "$LOG_FILE"
    sleep 1
    printf '%s\n' down-done >> "$LOG_FILE"
    ;;
  "compose -f compose.dev.yaml up --remove-orphans")
    printf '%s\n' up-start >> "$LOG_FILE"
    ;;
  *)
    printf 'unexpected docker args: %s\n' "$*" >&2
    exit 2
    ;;
esac
STUB
chmod +x "$TMP_DIR/bin/docker"

LOG_FILE="$TMP_DIR/docker.log"
PATH="$TMP_DIR/bin:$PATH" BACKEND_REFRESH_TEST_LOG="$LOG_FILE" "$ROOT/hack/refresh.sh"

build_start_line=$(grep -n '^build-start$' "$LOG_FILE" | cut -d: -f1)
down_start_line=$(grep -n '^down-start$' "$LOG_FILE" | cut -d: -f1)
build_done_line=$(grep -n '^build-done$' "$LOG_FILE" | cut -d: -f1)
up_start_line=$(grep -n '^up-start$' "$LOG_FILE" | cut -d: -f1)

if [ -z "$build_start_line" ] || [ -z "$down_start_line" ] || [ -z "$build_done_line" ] || [ -z "$up_start_line" ]; then
  printf 'missing expected docker calls\n' >&2
  cat "$LOG_FILE" >&2
  exit 1
fi

if [ "$down_start_line" -gt "$build_done_line" ]; then
  printf 'down did not start while build was still running\n' >&2
  cat "$LOG_FILE" >&2
  exit 1
fi

if [ "$up_start_line" -lt "$build_done_line" ]; then
  printf 'up started before build finished\n' >&2
  cat "$LOG_FILE" >&2
  exit 1
fi
