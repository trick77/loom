#!/usr/bin/env bash
# Coverage gates.
#
# Two independent checks per stack:
#   1. patch coverage  — lines this branch adds/changes must be >= PATCH_MIN.
#      Legacy untested code is deliberately ignored, so the gate is never red
#      for debt somebody else created.
#   2. project floor   — the overall total must not fall below the level already
#      achieved. Pins gains without ever being red on arrival.
#
# Patch coverage is line-based on both stacks (diff-cover), and the UI floor is
# lines (vitest thresholds). The Go floor is statement-based only because
# `go tool cover` exposes no line metric.
#
# Coverage profiles must already exist; run `make coverage` first (the Makefile
# `coverage-gate` target does both).
set -euo pipefail

BASE_REF="${1:-origin/master}"
PATCH_MIN="${PATCH_MIN:-80}"
# Floor, not a target: just under the level already achieved (76.2%) so it pins
# the gain without going red on rounding noise. Raise it as coverage climbs.
BACKEND_FLOOR="${BACKEND_FLOOR:-75}"

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root"

fail=0
checked=0
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"
mkdir -p coverage

# diff-cover prints "No lines with coverage information" and exits 0 when none of
# the report's paths match the diff. That is legitimate for a docs/CI-only PR, but
# it is also exactly what a broken path mapping looks like — and that bug already
# shipped once here (lcov paths vs --src-roots). So treat the message as a failure
# only when that stack's sources actually changed.
assert_matched() {
  local report="$1" label="$2" base
  shift 2
  base="$(git merge-base "$BASE_REF" HEAD)"
  if git diff --name-only "$base"...HEAD -- "$@" | grep -qv '_test\.go$' &&
    grep -q 'No lines with coverage information' "$report"; then
    echo "FAIL: $label sources changed but diff-cover matched no coverage data." >&2
    echo "      This usually means the report's paths do not match git's." >&2
    fail=1
  fi
}

# diff-cover needs the base commit present; CI checkouts are shallow by default.
if ! git rev-parse --verify --quiet "$BASE_REF" >/dev/null; then
  echo "error: base ref '$BASE_REF' not found. In CI, checkout needs fetch-depth: 0." >&2
  exit 2
fi

# --- backend ------------------------------------------------------------------
if [[ -f coverage/backend.out ]]; then
  checked=1
  # gocover-cobertura must run inside the module dir, otherwise it resolves no
  # package info and silently emits an empty report.
  (cd backend && go run github.com/boumenot/gocover-cobertura@v1.5.0 \
    < ../coverage/backend.out > ../coverage/backend.xml)

  echo "== backend patch coverage (>= ${PATCH_MIN}%) =="
  # --src-roots: report paths are relative to backend/, git paths are not.
  diff-cover coverage/backend.xml \
    --src-roots backend \
    --compare-branch "$BASE_REF" \
    --fail-under "$PATCH_MIN" \
    --format "markdown:coverage/backend-patch.md" || fail=1
  cat coverage/backend-patch.md >> "$summary" 2>/dev/null || true
  assert_matched coverage/backend-patch.md backend "backend/*.go"

  # `go tool cover` resolves packages via go.mod, so it must run inside backend/.
  total="$(cd backend && go tool cover -func=../coverage/backend.out |
    awk '/^total:/ {gsub(/%/,"",$3); print $3}')"
  echo "== backend project total: ${total}% (floor ${BACKEND_FLOOR}%) =="
  awk -v t="$total" -v f="$BACKEND_FLOOR" 'BEGIN { exit (t+0 < f+0) ? 1 : 0 }' || {
    echo "FAIL: backend total ${total}% is below floor ${BACKEND_FLOOR}%" >&2
    fail=1
  }
fi

# --- ui -----------------------------------------------------------------------
# The UI project floor is enforced by vitest's own coverage.thresholds in
# ui/vitest.config.ts, so only patch coverage is checked here.
if [[ -f ui/coverage/lcov.info ]]; then
  checked=1
  echo "== ui patch coverage (>= ${PATCH_MIN}%) =="
  # vitest writes SF: paths relative to ui/. --src-roots does NOT rewrite these
  # for lcov (it does for Cobertura's <sources>), and a mismatch makes diff-cover
  # report "no lines with coverage information" and pass vacuously. Rewrite them
  # to repo-root-relative so they match git's paths.
  sed 's|^SF:|SF:ui/|' ui/coverage/lcov.info > coverage/ui-lcov-rooted.info

  diff-cover coverage/ui-lcov-rooted.info \
    --compare-branch "$BASE_REF" \
    --fail-under "$PATCH_MIN" \
    --format "markdown:coverage/ui-patch.md" || fail=1
  cat coverage/ui-patch.md >> "$summary" 2>/dev/null || true
  assert_matched coverage/ui-patch.md ui "ui/src/*.ts" "ui/src/*.tsx"
fi

# A gate that checked nothing must not report success: if neither profile was
# produced (reporter moved, output dir changed), fail loudly instead of green.
if [[ "$checked" -eq 0 ]]; then
  echo "error: no coverage profiles found. Run 'make coverage' first." >&2
  exit 2
fi

exit "$fail"
