#!/bin/sh
#
# Start the backend (with built-in Tavily web search) for UI development,
# WITHOUT building the embedded frontend. Run the UI separately with Vite/HMR:
#
#   ./hack/dev-backend.sh                 # backend on http://localhost:8080
#   cd frontend && npm run dev            # UI with hot-reload on http://localhost:5173
#
# Open http://localhost:5173 (NOT :8080). The Vite proxy forwards /api to the
# backend, keeping API + streaming + the dev-auth session cookie same-origin.
#
# All required runtime vars (BACKEND_AUTH_MODE=dev, BACKEND_ADDR, BACKEND_SESSION_SECRET,
# BACKEND_DB_PATH, BACKEND_USERS_DIR, sidecar MCP URLs) are baked into compose.dev.yaml.
# For real chatting, set the optional chat vars in an uncommitted .env file at the
# repo root (Docker Compose reads it automatically):
#
#   BACKEND_CHAT_BASE_URL=https://your-openai-compatible-host/v1
#   BACKEND_CHAT_API_KEY=your-api-key
#   BACKEND_CHAT_MODEL=your-model
#
# Any extra args are passed straight to `docker compose up` (e.g. -d, --no-build).
set -eu

ROOT=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
cd "$ROOT"

exec docker compose -f compose.dev.yaml -f compose.dev-uihmr.yaml up --build --remove-orphans "$@"
