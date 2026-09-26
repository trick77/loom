#!/bin/sh
# Fails when loom names an LLM model or vendor outside configuration. Model
# facts live in llmwire profiles; loom picks models only through BACKEND_*_MODEL
# in .env (see AGENTS.md). Only tracked files count; docs/ holds dated specs
# and is history, not code.
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

if git grep -nIiwE 'mimo|glm|zai|xiaomi|z\.ai' -- . \
  ':!docs' ':!.env.example' ':!hack/check-model-names.sh'; then
  echo "model or vendor names outside configuration (move the fact into llmwire)"
  exit 1
fi
echo "check-model-names: ok"
