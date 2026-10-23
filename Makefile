.PHONY: build test fe-build fe-test fe-lint model-names run dev refresh docker-dev docker-dev-down tidy sandbox-escape-test

tidy:
	cd backend && go mod tidy

test:
	cd backend && go test ./...

fe-test:
	cd ui && npm run test

# ESLint (react-hooks) and prettier, as ship checks them.
fe-lint:
	cd ui && npm run lint

# Loom names no model outside configuration; model facts live in llmwire.
model-names:
	./scripts/check-model-names.sh

# vite builds into backend/web/dist and does not empty it (emptyOutDir: false):
# the old hashed bundles would be embedded too.
fe-build:
	find backend/web/dist -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
	cd ui && npm ci && npm run build

build: fe-build
	cd backend && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X github.com/trick77/ship/kit/version.Version=$$(git rev-parse --short HEAD 2>/dev/null || echo dev)" -o ../bin/loom ./cmd/loom

run:
	cd backend && go run ./cmd/loom

dev:
	./scripts/dev.sh

refresh:
	./scripts/refresh.sh

# Dockerized local dev with dev auth (no OIDC provider). Single embedded image, UI on http://localhost:8080.
docker-dev:
	docker compose -f compose.dev.yaml up --build --remove-orphans

docker-dev-down:
	docker compose -f compose.dev.yaml down

# Hostile-code checks against the run_python sandbox image, started with the
# production settings from compose.yaml on plain Docker.
sandbox-escape-test:
	./scripts/sandbox-escape-test.sh

# ship:begin (owned by ship init)
# The gate: everything CI checks, configured in ship.yaml. Master's ship, as the pre-push hook and the @master
# workflows run it: its sha asked of GitHub (a proxy lags on @master). A local ship: SHIP_CMD=<go-installed binary>,
# one name for here and the hook, so make check and the push gate never run different ships.
# Exactly refs/heads/master (ls-remote matches by suffix), a 40-hex sha or nothing runs, as in the workflows.
SHIP_CMD ?= sha=$$(git ls-remote https://github.com/trick77/ship refs/heads/master | \
		awk '$$2 == "refs/heads/master" { print $$1 }'); \
	echo "$$sha" | grep -Eqx '[0-9a-f]{40}' || \
		{ echo "cannot resolve ship's master on GitHub: '$$sha' (git ls-remote, error above)"; exit 1; }; \
	go run github.com/trick77/ship@$$sha

.PHONY: check hooks lint

check:
	$(SHIP_CMD) check

hooks:
	$(SHIP_CMD) hooks

# Quick lint while editing, at the version check runs.
lint:
	$(SHIP_CMD) lint
# ship:end
