-include .env
export

MIGRATIONS_DIR := migrations
OPENAPI_SPEC := contracts/openapi/trip-service.openapi.yaml

.PHONY: run migrate migrate-down migrate-status generate docker-build docker-run lint fmt test test-integration cover

run:
	go run ./cmd/trip-service

migrate:
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" up

migrate-down:
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" down-to 0

migrate-status:
	go tool goose -dir $(MIGRATIONS_DIR) postgres "$(DATABASE_URL)" status

generate:
	go tool oapi-codegen \
		-generate types,chi-server \
		-package generated \
		-o internal/generated/api.gen.go \
		-include-operation-ids createTrip,getTrip,finishTrip,health,ready \
		$(OPENAPI_SPEC)
	go generate ./...

docker-build:
	docker build -f deploy/Dockerfile -t trip-service:lab1 .

docker-run:
	docker run --rm --name trip-service --network kind -p 8080:8080 \
		--env-file .env \
		-e DATABASE_URL='postgres://tripgo:tripgo@tripgo-local-control-plane:30203/tripgo?sslmode=disable' \
		trip-service:lab1

lint:
	golangci-lint run ./...

fmt:
	golangci-lint fmt ./...

test:
	go test -race -cover ./...

test-integration:
	go test -race -count=1 -tags integration ./...

cover:
	go test -coverpkg=./internal/usecase/...,./internal/domain/... -coverprofile=coverage.out ./internal/usecase/... ./internal/domain/...
	go tool cover -func=coverage.out | tail -n 1