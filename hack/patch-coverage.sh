#!/usr/bin/env bash
# hack/patch-coverage.sh [base-ref]
#
# Patch coverage: the lines this branch adds or changes must be at least
# PATCH_MIN% covered. Complements hack/coverage-gate.sh, which enforces the
# absolute project floor — the two answer different questions:
#
#   coverage-gate.sh   "is the codebase as a whole tested enough?"   (75% floor)
#   patch-coverage.sh  "is the code I just wrote tested?"            (80% patch)
#
# The floor alone lets a large well-tested codebase absorb untested new code
# without ever going red. The patch gate alone lets legacy debt sit forever.
# Both, together, pin the gain without punishing anyone for debt they inherited.
#
# Ported from loom, which has run this since its early days.
#
# Coverage reports must already exist — CI produces them before calling this.
set -euo pipefail

BASE_REF="${1:-origin/master}"
PATCH_MIN="${PATCH_MIN:-80}"

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

fail=0
checked=0
summary="${GITHUB_STEP_SUMMARY:-/dev/null}"
mkdir -p coverage

# diff-cover needs the base commit present; CI checkouts are shallow by default.
if ! git rev-parse --verify --quiet "$BASE_REF" >/dev/null; then
  echo "patch-coverage: base ref '$BASE_REF' not found." >&2
  echo "  In CI, actions/checkout needs fetch-depth: 0." >&2
  exit 2
fi

# diff-cover prints "No lines with coverage information" and exits 0 when none of
# the report's paths match the diff. That is legitimate for a docs-only PR, but it
# is also exactly what a broken path mapping looks like — and that bug has shipped
# before (lcov SF: paths vs git paths). So treat the message as a failure only
# when that stack's sources actually changed.
assert_matched() {
  local report="$1" label="$2" base changed
  shift 2
  base="$(git merge-base "$BASE_REF" HEAD)"
  # Test files are excluded from coverage reports by design, so a diff touching
  # only tests legitimately matches no coverage data — don't flag that.
  # Count rather than `grep -qv`: under ugrep (a common macOS `grep`) the -q/-v
  # combination returns 1 even when non-matching lines exist, which would
  # silently disable this guard locally while it still works under GNU grep.
  changed="$(git diff --name-only "$base"...HEAD -- "$@" |
    grep -cvE '(_test\.go|\.test\.tsx?)$' || true)"

  if [[ "${changed:-0}" -gt 0 ]] &&
    grep -q 'No lines with coverage information' "$report"; then
    echo "patch-coverage: FAIL — $label sources changed but diff-cover matched" >&2
    echo "  no coverage data. This usually means the report's paths do not" >&2
    echo "  match git's paths." >&2
    fail=1
  fi
}

# --- backend ------------------------------------------------------------------
# CI already converts the coverprofile to Cobertura for hack/coverage-gate.sh;
# reuse that artifact rather than regenerating it.
if [[ -f coverage/backend.xml ]]; then
  checked=1
  echo "== backend patch coverage (>= ${PATCH_MIN}%) =="
  # gocover-cobertura writes an ABSOLUTE path into <sources>, e.g.
  # /home/runner/work/peeq/peeq/backend. diff-cover joins that with each
  # class's module-relative filename, producing an absolute path that never
  # matches git's repo-relative paths — so every file silently misses and the
  # gate passes vacuously at "No lines with coverage information".
  #
  # --src-roots does NOT override this; the embedded <sources> wins. Rewriting
  # the element to a repo-relative path is what actually makes the match work.
  # Verified: with the absolute path diff-cover reports 0 matched lines; with
  # the rewrite it correctly reports the changed lines and their coverage.
  sed 's|<source>.*</source>|<source>backend</source>|' \
    coverage/backend.xml > coverage/backend-rooted.xml

  diff-cover coverage/backend-rooted.xml \
    --compare-branch "$BASE_REF" \
    --fail-under "$PATCH_MIN" \
    --format "markdown:coverage/backend-patch.md" || fail=1
  cat coverage/backend-patch.md >> "$summary" 2>/dev/null || true
  assert_matched coverage/backend-patch.md backend "backend/*.go"
fi

# --- ui -----------------------------------------------------------------------
if [[ -f coverage/ui/lcov.info ]]; then
  checked=1
  echo "== ui patch coverage (>= ${PATCH_MIN}%) =="
  # vitest writes SF: paths relative to ui/. --src-roots does NOT rewrite these
  # for lcov (it only does so for Cobertura's <sources>), and a mismatch makes
  # diff-cover report "no lines with coverage information" and pass vacuously.
  # Rewrite them to repo-root-relative so they match git's paths.
  sed 's|^SF:|SF:ui/|' coverage/ui/lcov.info > coverage/ui-lcov-rooted.info

  diff-cover coverage/ui-lcov-rooted.info \
    --compare-branch "$BASE_REF" \
    --fail-under "$PATCH_MIN" \
    --format "markdown:coverage/ui-patch.md" || fail=1
  cat coverage/ui-patch.md >> "$summary" 2>/dev/null || true
  assert_matched coverage/ui-patch.md ui "ui/src/*.ts" "ui/src/*.tsx"
fi

# A gate that checked nothing must not report success: if neither report was
# produced (reporter moved, output dir changed), fail loudly instead of green.
if [[ "$checked" -eq 0 ]]; then
  echo "patch-coverage: no coverage reports found under coverage/." >&2
  echo "  Run the tests first — CI does this before invoking the gate." >&2
  exit 2
fi

exit "$fail"
