-include .env
export

MIGRATIONS_DIR := migrations
OPENAPI_SPEC := contracts/openapi/trip-service.openapi.yaml
GOLANGCI_LINT_VERSION := v2.15.0
GOLANGCI_LINT := $(CURDIR)/bin/golangci-lint/$(GOLANGCI_LINT_VERSION)/golangci-lint
GREMLINS_VERSION := v0.6.0
GREMLINS := $(CURDIR)/bin/gremlins/$(GREMLINS_VERSION)/gremlins
MUTATION_REPORT_DIR ?= reports/mutation/latest
IMAGE ?= trip-service:lab3
DOCKER_NODE ?= tripgo-local-control-plane
DOCKER_NETWORK ?= kind
DOCKER_CONTAINER_NAME ?= trip-service
DOCKER_HTTP_PORT ?= 8080
DOCKER_ENV_FILE ?= .env
DOCKER_DATABASE_URL ?= postgres://tripgo:tripgo@$(DOCKER_NODE):30303/tripgo?sslmode=disable
DOCKER_PUSH_HTTP_URL ?= http://$(DOCKER_NODE):30308
DOCKER_OTLP_ENDPOINT ?= http://$(DOCKER_NODE):30306

.PHONY: build run migrate migrate-down migrate-status generate docker-build docker-run lint fmt test test-integration test-position-pool-load cover test-mutation

run:
	go run ./cmd/trip-service

build:
	go build -o bin/trip-service ./cmd/trip-service

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
		-include-operation-ids createTrip,getTrip,finishTrip,createTripPosition,listTripPositions,health,ready \
		$(OPENAPI_SPEC)
	go generate ./...

docker-build:
	docker build -f deploy/Dockerfile -t "$(IMAGE)" .

docker-run:
	docker run --rm --name "$(DOCKER_CONTAINER_NAME)" --network "$(DOCKER_NETWORK)" -p "$(DOCKER_HTTP_PORT):8080" \
		--env-file "$(DOCKER_ENV_FILE)" \
		-e HTTP_ADDR=:8080 \
		-e DATABASE_URL='$(DOCKER_DATABASE_URL)' \
		-e PUSH_HTTP_URL='$(DOCKER_PUSH_HTTP_URL)' \
		-e OTEL_EXPORTER_OTLP_ENDPOINT='$(DOCKER_OTLP_ENDPOINT)' \
		"$(IMAGE)"

$(GOLANGCI_LINT):
	mkdir -p "$(@D)"
	curl -sSfL https://golangci-lint.run/install.sh -o "$(@D)/install.sh"
	sh "$(@D)/install.sh" -b "$(@D)" $(GOLANGCI_LINT_VERSION)
	rm "$(@D)/install.sh"

lint: $(GOLANGCI_LINT)
	"$(GOLANGCI_LINT)" run ./...

fmt: $(GOLANGCI_LINT)
	"$(GOLANGCI_LINT)" fmt ./...

test:
	go test -race -cover ./...

test-integration:
	go test -race -count=1 -tags integration ./...

test-position-pool-load: build
	python3 scripts/position-pool-load.py

$(GREMLINS):
	GOBIN="$(@D)" go install github.com/go-gremlins/gremlins/cmd/gremlins@$(GREMLINS_VERSION)

test-mutation: $(GREMLINS)
	mkdir -p "$(MUTATION_REPORT_DIR)"
	"$(GREMLINS)" unleash --config .gremlins.yaml --output "$(MUTATION_REPORT_DIR)/domain.json" "$(CURDIR)/internal/domain"
	"$(GREMLINS)" unleash --config .gremlins.yaml --output "$(MUTATION_REPORT_DIR)/usecase.json" "$(CURDIR)/internal/usecase"

cover:
	go test -coverpkg=./internal/usecase/...,./internal/domain/... -coverprofile=coverage.out ./internal/usecase/... ./internal/domain/...
	go tool cover -func=coverage.out | tail -n 1
