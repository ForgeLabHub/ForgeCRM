# Руководство разработчика ForgeCRM

> Для нового разработчика: от клонирования до работающей системы. Проверено на
> `main` @ `3424b3e`.

## 1. Требования

| Инструмент | Версия | Откуда |
|---|---|---|
| Go | 1.26.10 | `backend/go.mod`, `BUILD.md` |
| Node.js | 24.x | `frontend/package.json` |
| PostgreSQL | 17 | Docker-образ `postgres:17-alpine` |
| Docker | — | для compose и интеграционных тестов |

## 2. Клонирование и быстрый старт

```bash
git clone https://github.com/ForgeLabHub/ForgeCRM.git
cd ForgeCRM
./scripts/doctor.sh          # проверит окружение
cp .env.example .env         # отредактируйте локально; не коммитьте
docker compose up -d --build
curl -s localhost:8080/health
```

## 3. Сборка и тесты backend

```bash
cd backend
export PATH=$PATH:/usr/local/go/bin GOTOOLCHAIN=local
go build ./...
go vet ./...
gofmt -l ./cmd ./internal            # должно быть пусто
go test -race -count=1 ./...
```

Интеграционные тесты (реальный PostgreSQL и Redis на отдельных портах):

```bash
./scripts/integration-env.sh up
export FORGECRM_TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:55432/forgecrm_test?sslmode=disable'
export FORGECRM_TEST_REDIS_URL='redis://127.0.0.1:56379/1'
go test -tags=integration -count=1 ./...
```

## 4. Frontend

```bash
cd frontend
npm ci
npm run typecheck
npm run lint
npm run test
npm run build
```

## 5. Генераторы и гейты (не править сгенерированное руками)

```bash
cd backend
go run ./cmd/openapi-gen            # перегенерировать docs/api/openapi.json
go run ./cmd/facts-gen -repo .. -write   # перегенерировать project-facts.md
cd ..
./scripts/docs-check.sh             # разделы модулей, ссылки, согласованность
./scripts/module-docs-check.sh      # манифесты переносимости модулей
./scripts/extract-test/run.sh       # 36 модулей извлекаются
```

## 6. Куда добавлять код

| Что | Куда |
|---|---|
| Новый HTTP-маршрут | `internal/api/<area>_handlers.go` + строка в `contract.go` |
| Новое доменное правило | `internal/crm` |
| Новое событие | `internal/outbox` (`Event*`) |
| Новая миграция | `internal/migrations/NNNN_name.{up,down}.sql` |
| Новая фоновая задача | `internal/jobs` + обработчик в `cmd/forgecrm-worker` |
| Новый внешний адаптер | интерфейс + адаптер + contract-тест |
| Новая страница | `frontend/src/pages/`, клиент в `frontend/src/api/` |

## 7. Правила, которые проверяет CI

- `gofmt`, `go vet`, unit, integration, race.
- `archtest` — правила зависимостей (см. `DEPENDENCY_RULES.md`).
- `extract-test` — 36 модулей извлекаются.
- `openapi-check`, `facts-check`, `docs-check`, `module-docs-check`.
- frontend lint/typecheck/test/build.
- gitleaks, govulncheck, gosec.

## 8. Локальный запуск бинарников без Docker

```bash
cd backend
go build -o /tmp/forgecrm-api ./cmd/forgecrm-api
FORGECRM_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm?sslmode=disable' \
FORGECRM_SECRET_KEY='dev-secret-key-0123456789abcdef0123456789abcdef' \
FORGECRM_ENV=development FORGECRM_HTTP_PORT=8080 /tmp/forgecrm-api
```

## 9. Как читать архитектуру

Начните с `docs/architecture/README.md`, затем `SYSTEM_OVERVIEW.md` и
`MODULAR_MONOLITH.md`. Для переноса модуля — `PORTABLE_MODULES.md`.
