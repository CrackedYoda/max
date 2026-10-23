include .env
export

.PHONY: up down migrate migrate-down sqlc run test lint

up:
	docker compose up -d --wait

down:
	docker compose down

migrate:
	go run ./cmd/migrate up

migrate-down:
	go run ./cmd/migrate down 1

sqlc:
	sqlc generate

run:
	go run ./cmd/api

test:
	go test ./...

lint:
	gofmt -l . && go vet ./...
