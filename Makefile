.PHONY: build test coverage coverage-gate backend-coverage fe-build fe-test fe-coverage run dev refresh docker-dev docker-dev-down tidy

tidy:
	cd backend && go mod tidy

test:
	cd backend && go test ./...

coverage: backend-coverage fe-coverage

# Enforce both gates. The project floor (hack/coverage-floors) is absolute and
# per-stack; patch coverage is measured against a base ref, default origin/master.
# Requires diff-cover: pip install diff-cover==10.3.0
coverage-gate: coverage
	./hack/coverage-gate.sh backend
	./hack/coverage-gate.sh ui
	./hack/patch-coverage.sh $(BASE_REF)

# -coverpkg=./... attributes coverage across package boundaries. Without it code
# exercised only by another package's tests (the httpapi tests drive chat/store/llm)
# is reported as uncovered.
#
# -race needs cgo, which is independent of the CGO_ENABLED=0 invariant the
# release build relies on.
#
# The Cobertura conversion is what makes a LINE metric available: `go tool cover`
# reports statements only and exposes no line percentage. It also merges the
# duplicate blocks -coverpkg emits (one set per test binary), which a naive sum
# over the raw profile gets badly wrong.
backend-coverage:
	mkdir -p coverage
	cd backend && CGO_ENABLED=1 go test -race ./... -covermode=atomic -coverpkg=./... -coverprofile=../coverage/backend.out
	cd backend && go run github.com/boumenot/gocover-cobertura@v1.5.0 < ../coverage/backend.out > ../coverage/backend.xml

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

# Dockerized local dev with dev auth (no OIDC provider). Single embedded image, UI on http://localhost:8080.
docker-dev:
	docker compose -f compose.dev.yaml up --build --remove-orphans

docker-dev-down:
	docker compose -f compose.dev.yaml down
