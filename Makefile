.PHONY: build test coverage coverage-gate backend-coverage fe-build fe-test fe-coverage run dev refresh docker-dev docker-dev-down tidy

tidy:
	cd backend && go mod tidy

test:
	cd backend && go test ./...

coverage: backend-coverage fe-coverage

# Enforce the gates against a base ref (default origin/master).
# Requires diff-cover: pip install diff-cover
coverage-gate: coverage
	./hack/coverage-gate.sh $(BASE_REF)

backend-coverage:
	mkdir -p coverage
	# -coverpkg=./... attributes coverage across package boundaries. Without it
	# code exercised only by another package's tests (the httpapi tests drive
	# chat/store/llm) is reported as uncovered.
	cd backend && go test ./... -covermode=atomic -coverpkg=./... -coverprofile=../coverage/backend.out
	cd backend && go tool cover -func=../coverage/backend.out

fe-test:
	cd ui && npm run test -- --run

fe-coverage:
	cd ui && npm run test:coverage

fe-build:
	cd ui && npm ci && npm run build

build: fe-build
	cd backend && CGO_ENABLED=0 go build -o ../bin/loom ./cmd/loom

run:
	cd backend && go run ./cmd/loom

dev:
	./hack/dev.sh

refresh:
	./hack/refresh.sh

# Dockerized local dev with dev auth (no authentik). Single embedded image, UI on http://localhost:8080.
docker-dev:
	docker compose -f compose.dev.yaml up --build --remove-orphans

docker-dev-down:
	docker compose -f compose.dev.yaml down
