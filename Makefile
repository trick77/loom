.PHONY: build test fe-build fe-test run dev refresh docker-dev docker-dev-down tidy

tidy:
	cd backend && go mod tidy

test:
	cd backend && go test ./...

fe-test:
	cd frontend && npm run test -- --run

fe-build:
	cd frontend && npm ci && npm run build

build: fe-build
	cd backend && CGO_ENABLED=0 go build -o ../bin/spark ./cmd/spark

run:
	cd backend && go run ./cmd/spark

dev:
	./hack/dev.sh

refresh:
	./hack/refresh.sh

# Dockerized local dev with dev auth (no authentik). Single embedded image, UI on http://localhost:8080.
docker-dev:
	docker compose -f compose.dev.yaml up --build

docker-dev-down:
	docker compose -f compose.dev.yaml down
