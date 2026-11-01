# ForgeCRM — единая точка входа для сборки и проверок.
#
# Принцип: цель Makefile не «спрятать» команды, а дать один стабильный
# контракт (`make <target>`), под которым стоят обычные, читаемые команды.
# Каждая цель работает из чистого клона и не требует секретов, кроме тех,
# что явно помечены как интеграционные.
#
#   make help              список целей
#   make doctor            проверка инструментов, версий и сервисов
#   make build             артефакты: API, worker, frontend
#   make test              unit-тесты без базы данных
#   make test-integration  тесты против реального PostgreSQL
#   make verify            полная матрица проверок локально
#
# Версии инструментов должны совпадать с .github/workflows/ci.yml.

SHELL := /usr/bin/env bash
.DEFAULT_GOAL := help

GO_VERSION      := 1.26.10
NODE_VERSION    := 22
COMPOSE         ?= docker compose
BACKEND_DIR     := backend
FRONTEND_DIR    := frontend
BIN_DIR         := bin
API_BIN         := $(BIN_DIR)/forgecrm-api
WORKER_BIN      := $(BIN_DIR)/forgecrm-worker
TEST_DATABASE_URL ?= postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable
BLITZ_VERIFY_IMAGE ?= forgecrm-blitz-verify:latest

# Команды различаются для локальной машины и контейнера: docker compose run
# запускает миграции в сети compose, прямой бинарник — по FORGECRM_DATABASE_URL.
COMPOSE_UP_ARGS ?=

.PHONY: help doctor setup build build-api build-worker build-frontend \
        fmt fmt-check vet lint openapi openapi-check facts facts-check \
        test test-unit test-frontend test-integration test-e2e \
        migrate migrate-status compose-up compose-down compose-logs smoke-test \
        blitz-verify \
        backup restore docs-check docs-claims verify clean package

help: ## Показать список целей
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) \
	  | awk 'BEGIN {FS = ":.*?## "}; {printf "  \033[36m%-18s\033[0m %s\n", $$1, $$2}'

## --- Окружение -------------------------------------------------------------

doctor: ## Проверить инструменты, версии и доступность сервисов (ничего не меняет)
	@./scripts/doctor.sh

setup: ## Создать .env, секрет, поднять базу, применить миграции, создать админа
	@./scripts/setup.sh $(ADMIN_EMAIL)

## --- Уровень A: исходный код ----------------------------------------------

fmt: ## Отформатировать Go и frontend
	cd $(BACKEND_DIR) && gofmt -w ./cmd ./internal
	cd $(FRONTEND_DIR) && npm run lint -- --fix >/dev/null 2>&1 || true

fmt-check: ## Проверить форматирование (падает, если файлы не отформатированы)
	@out="$$(cd $(BACKEND_DIR) && gofmt -l ./cmd ./internal)"; \
	if [ -n "$$out" ]; then echo "не отформатировано:"; echo "$$out"; exit 1; fi
	@echo "gofmt: ok"

vet: ## go vet
	cd $(BACKEND_DIR) && go vet ./...

lint: fmt-check vet ## Все статические проверки backend
	cd $(FRONTEND_DIR) && npm run lint
	cd $(FRONTEND_DIR) && npm run typecheck

test-unit: ## Unit-тесты Go без базы данных
	cd $(BACKEND_DIR) && go test -race -count=1 ./...

test-frontend: ## Unit-тесты frontend
	cd $(FRONTEND_DIR) && npm run test

test: test-unit test-frontend ## Unit-тесты backend + frontend

## --- Уровень B: артефакты --------------------------------------------------

build: build-api build-worker build-frontend ## Собрать все артефакты локально

build-api: ## Собрать бинарник API
	@mkdir -p $(BIN_DIR)
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build -trimpath -o ../$(API_BIN) ./cmd/forgecrm-api
	@echo "built $(API_BIN)"

build-worker: ## Собрать бинарник worker
	@mkdir -p $(BIN_DIR)
	cd $(BACKEND_DIR) && CGO_ENABLED=0 go build -trimpath -o ../$(WORKER_BIN) ./cmd/forgecrm-worker
	@echo "built $(WORKER_BIN)"

build-frontend: ## Production-сборка React
	cd $(FRONTEND_DIR) && npm ci && npm run build
	@echo "built $(FRONTEND_DIR)/dist"

openapi: ## Перегенерировать docs/api/openapi.json из роутера
	cd $(BACKEND_DIR) && go run ./cmd/openapi-gen
	@echo "docs/api/openapi.json обновлён"

openapi-check: ## Убедиться, что спецификация не разошлась с кодом (как в CI)
	cd $(BACKEND_DIR) && go run ./cmd/openapi-gen -check

facts: ## Перегенерировать блок FACTS:AUTO в docs/verification/project-facts.md
	cd $(BACKEND_DIR) && go run ./cmd/facts-gen -repo .. -write

facts-check: ## Убедиться, что project-facts.md не разошёлся с кодом (как в CI)
	cd $(BACKEND_DIR) && go run ./cmd/facts-gen -repo .. -check

docs-check: ## Проверить структуру документации, ссылки и матрицу проверок
	@./scripts/docs-check.sh

docs-claims: ## Проверить, что числа и статусы в документации совпадают с кодом
	@./scripts/check-doc-claims.sh

forge-check: ## Проверить, что команды forge.sh работают и совпадают со справкой
	@./scripts/forge-check.sh

## --- Уровень C: интеграционная среда --------------------------------------

test-integration: ## Тесты против реального PostgreSQL (нужен запущенный compose)
	# -p 1 обязателен: интеграционные пакеты делят одну схему и сбрасывают её,
	# поэтому параллельный запуск пакетов ломает друг другу базу.
	cd $(BACKEND_DIR) && FORGECRM_TEST_DATABASE_URL='$(TEST_DATABASE_URL)' \
	  go test -tags=integration -count=1 -p 1 ./...

test-e2e: ## Playwright E2E (Chromium, реальный API и база)
	cd $(FRONTEND_DIR) && npm run e2e

test-extract: ## Каждый модуль собирается вне монолита
	./scripts/extract-test/run.sh

test-constructor: ## Каждый рецепт конструктора даёт рабочий проект
	./scripts/constructor-test/run.sh

migrate: ## Применить миграции через API-контейнер
	@./scripts/migrate.sh up

migrate-status: ## Текущая версия схемы
	@./scripts/migrate.sh status

compose-up: ## Поднять весь стек
	$(COMPOSE) up -d $(COMPOSE_UP_ARGS)

compose-down: ## Остановить стек (данные сохраняются; тома не удаляются)
	$(COMPOSE) down

compose-reset: ## СНЕСТИ стек вместе с данными — только осознанно
	$(COMPOSE) down -v

compose-logs: ## Логи всех сервисов
	$(COMPOSE) logs -f --tail=200

smoke-test: ## Проверить живой инстанс
	@./scripts/smoke-test.sh $(SMOKE_URL)

blitz-verify: ## Воспроизвести развёртывание blitz.cloud на чистой БД (нужен Docker)
	@./scripts/blitz-verify.sh $(BLITZ_VERIFY_IMAGE)

## --- Эксплуатация ----------------------------------------------------------

backup: ## Резервная копия БД и файлов
	@./scripts/backup.sh $(BACKUP_DIR)

restore: ## Восстановление из дампа (требует DUMP=... и --yes-really)
	@./scripts/restore.sh $(DUMP) --yes-really

## --- Сводные цели ----------------------------------------------------------

verify: lint test openapi-check facts-check docs-check forge-check ## Локальная матрица без контейнеров
	@echo "verify: ok (сборка и интеграционные уровни проверяются в CI)"

package: build ## Собрать релизные артефакты и контрольные суммы
	@mkdir -p dist
	cp $(API_BIN) $(WORKER_BIN) dist/
	cd $(FRONTEND_DIR) && tar -czf ../dist/forgecrm-frontend.tar.gz dist
	cd dist && sha256sum * > SHA256SUMS
	@echo "dist/: $(shell git rev-parse --short HEAD 2>/dev/null || echo no-git)"

clean: ## Удалить артефакты сборки
	rm -rf $(BIN_DIR) dist
	cd $(FRONTEND_DIR) && rm -rf dist node_modules/.vite
