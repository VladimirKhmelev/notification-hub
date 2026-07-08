.PHONY: up down logs build test test-integration

up:
	docker-compose up --build -d

down:
	docker-compose down

logs:
	docker-compose logs -f

build:
	go build -o bin/api ./cmd/api

# unit tests (no DB required)
test:
	go test ./...

# integration tests (requires running docker-compose)
test-integration:
	DATABASE_URL=postgres://hub:hub@localhost:5432/hub?sslmode=disable go test ./...
