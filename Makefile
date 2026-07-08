.PHONY: up down logs build

up:
	docker-compose up --build -d

down:
	docker-compose down

logs:
	docker-compose logs -f

build:
	go build -o bin/api ./cmd/api
