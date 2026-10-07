#!/usr/bin/env bash
# scripts/merge-sandbox-coverage.sh [raw-dir]
#
# Folds the counters of the -cover loom-sandbox binary (written by
# scripts/sandbox-escape-test.sh under SANDBOX_COVER_DIR) into the backend
# coverage report, then rebuilds the Cobertura XML the gates read. The
# runner's uid-drop and seccomp code only runs as root inside the
# container, so `go test` alone can never cover it.
#
# Run after `make backend-coverage`. Both profiles use covermode atomic;
# gocover-cobertura merges the blocks they share.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
RAW="${1:-coverage/sandbox-raw}"

[[ -f coverage/backend.out ]] || { echo "merge-sandbox-coverage: run make backend-coverage first" >&2; exit 2; }
[[ -d "$RAW" ]] || { echo "merge-sandbox-coverage: no counters in $RAW" >&2; exit 2; }

(cd backend && go tool covdata textfmt -i="$ROOT/$RAW" -o "$ROOT/coverage/sandbox.out")
if ! head -1 coverage/sandbox.out | grep -qx 'mode: atomic'; then
  echo "merge-sandbox-coverage: sandbox profile is not covermode atomic" >&2
  exit 2
fi
# cover_on.go is the coverage hook itself, built only with the sandboxcover
# tag; the converter cannot resolve a file outside the default build.
tail -n +2 coverage/sandbox.out | grep -v '/cmd/loom-sandbox/cover_on\.go:' >> coverage/backend.out
(cd backend && go run github.com/boumenot/gocover-cobertura@v1.5.0 < ../coverage/backend.out > ../coverage/backend.xml)
echo "merge-sandbox-coverage: merged $(($(wc -l < coverage/sandbox.out) - 1)) sandbox blocks"
