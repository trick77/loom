.PHONY: build test coverage backend-coverage fe-build fe-test fe-coverage run dev refresh docker-dev docker-dev-down tidy

tidy:
	cd backend && go mod tidy

test:
	cd backend && go test ./...

coverage: backend-coverage fe-coverage

backend-coverage:
	mkdir -p coverage
	cd backend && go test ./... -covermode=atomic -coverprofile=../coverage/backend.out
	cd backend && go tool cover -func=../coverage/backend.out

fe-test:
	cd frontend && npm run test -- --run

fe-coverage:
	cd frontend && npm run test:coverage

fe-build:
	cd frontend && npm ci && npm run build

build: fe-build
	cd backend && CGO_ENABLED=0 go build -o ../bin/slop ./cmd/slop

run:
	cd backend && go run ./cmd/slop

dev:
	./hack/dev.sh

refresh:
	./hack/refresh.sh

# Dockerized local dev with dev auth (no authentik). Single embedded image, UI on http://localhost:8080.
docker-dev:
	docker compose -f compose.dev.yaml up --build --remove-orphans

docker-dev-down:
	docker compose -f compose.dev.yaml down
