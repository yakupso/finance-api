.DEFAULT_GOAL := help

# Локальные команды берут DSN из .env, если он есть, иначе из окружения
ifneq (,$(wildcard .env))
include .env
export
endif

BIN_DIR   := bin
API_BIN   := $(BIN_DIR)/api
MIGR_BIN  := $(BIN_DIR)/migrator
COVERAGE  := coverage.out

.PHONY: help
help: ## Список команд
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
		| awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-22s\033[0m %s\n", $$1, $$2}'

# ---------------------------------------------------------------- сборка ----

.PHONY: build
build: ## Собрать оба бинаря в ./bin
	@mkdir -p $(BIN_DIR)
	CGO_ENABLED=0 go build -trimpath -o $(API_BIN)  ./cmd/api
	CGO_ENABLED=0 go build -trimpath -o $(MIGR_BIN) ./cmd/migrator

.PHONY: run
run: ## Запустить API локально (нужен доступный PostgreSQL)
	go run ./cmd/api

.PHONY: tidy
tidy: ## go mod tidy
	go mod tidy

# Миграции
.PHONY: migrate-up
migrate-up: ## Применить все миграции
	go run ./cmd/migrator up

.PHONY: migrate-down
migrate-down: ## Откатить последнюю миграцию
	go run ./cmd/migrator down

.PHONY: migrate-reset
migrate-reset: ## Откатить все миграции
	go run ./cmd/migrator reset

.PHONY: migrate-status
migrate-status: ## Показать статус миграций
	go run ./cmd/migrator status

.PHONY: seed
seed: ## Залить демо-данные из scripts/seed.sql
	psql "$(DATABASE_URL)" -v ON_ERROR_STOP=1 -f scripts/seed.sql

# Тесты
.PHONY: test
test: ## Unit-тесты (быстрые, без Docker)
	go test -race ./...

.PHONY: test-integration
test-integration: ## Интеграционные тесты (поднимают PostgreSQL через testcontainers)
	go test -race -count=1 -tags=integration ./...

.PHONY: test-all
test-all: ## Все тесты с отчётом о покрытии
	# -coverpkg обязателен: без него код, вызываемый из тестов другого пакета
	# (например, middleware из тестов транспорта), показывает нулевое покрытие.
	go test -race -count=1 -tags=integration \
		-coverpkg=./internal/... -coverprofile=$(COVERAGE) -covermode=atomic ./internal/...
	go tool cover -func=$(COVERAGE) | tail -n 1

.PHONY: cover
cover: test-all ## Открыть HTML-отчёт о покрытии
	go tool cover -html=$(COVERAGE) -o coverage.html
	@echo "coverage.html готов"

# Линтеры
.PHONY: lint
lint: ## Запустить golangci-lint
	golangci-lint run ./...

.PHONY: vet
vet: ## go vet
	go vet ./...

.PHONY: fmt
fmt: ## Отформатировать код
	gofmt -s -w .

.PHONY: openapi-lint
openapi-lint: ## Провалидировать OpenAPI-спецификацию (нужен npx)
	npx --yes @redocly/cli@latest lint --config .redocly.yaml api/openapi.yaml

# Docker
.PHONY: docker-up
docker-up: ## Поднять весь стек (postgres + миграции + api)
	docker compose up --build -d

.PHONY: docker-up-docs
docker-up-docs: ## Поднять стек вместе со Swagger UI на :8081
	docker compose --profile docs up --build -d

.PHONY: docker-down
docker-down: ## Остановить стек
	docker compose down

.PHONY: docker-clean
docker-clean: ## Остановить стек и удалить том с данными БД
	docker compose --profile docs down -v

.PHONY: docker-logs
docker-logs: ## Логи API
	docker compose logs -f api

.PHONY: clean
clean: ## Удалить артефакты сборки
	rm -rf $(BIN_DIR) $(COVERAGE) coverage.html
