.PHONY: build test fe-build fe-test run dev tidy

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
