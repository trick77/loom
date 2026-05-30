.PHONY: build test fe-build fe-test run tidy

tidy:
	cd backend && go mod tidy

test:
	cd backend && go test ./...

fe-test:
	cd frontend && npm run test -- --run

fe-build:
	cd frontend && npm ci && npm run build

build: fe-build
	cd backend && CGO_ENABLED=0 go build -o ../bin/eve ./cmd/eve

run:
	cd backend && go run ./cmd/eve
