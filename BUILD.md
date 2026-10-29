# Сборка: от чистой машины до работающего продукта

Цель: новый разработчик или агент клонирует репозиторий, читает этот файл, собирает
всё с нуля, проверяет каждый уровень отдельно и получает тот же результат, что и CI.

## Закреплённые версии

| Инструмент | Версия | Источник истины |
| --- | --- | --- |
| Go | `1.26.10` | `backend/go.mod`, `GO_VERSION` в `ci.yml`, `scripts/doctor.sh` |
| Node.js | `22.x` | `ci.yml` |
| npm-зависимости | lockfile | `frontend/package-lock.json` (`npm ci`) |
| PostgreSQL | `17-alpine` | `docker-compose.yml`, сервис в `ci.yml` |
| Redis | `7-alpine` | `docker-compose.yml` (опционален для продукта) |
| nginx | `1.27-alpine` | `docker-compose.yml` |

Правило: версия, указанная в трёх местах, должна совпадать во всех трёх. Расхождение —
дефект воспроизводимости, а не мелочь.

## Уровень A — исходный код

Не требует базы данных и секретов.

```bash
make doctor        # инструменты, версии, .env, порты — ничего не меняет
make fmt-check     # gofmt
make vet           # go vet
make lint          # fmt + vet + eslint + typecheck
make test-unit     # go test -race ./... (включая archtest)
make test-frontend # vitest
```

## Уровень B — артефакты

```bash
make build           # API + worker + frontend
make openapi-check   # спецификация не разошлась с кодом
make package         # dist/ + SHA256SUMS
```

Артефакты: `bin/forgecrm-api`, `bin/forgecrm-worker`, `frontend/dist/`.
Бинарники собираются с `CGO_ENABLED=0 -trimpath` — переносимы между Linux-хостами и
воспроизводимы по содержимому.

## Уровень C — интеграционная среда

Нужен PostgreSQL (и желательно Redis). Секреты не нужны, кроме тестовых значений.

```bash
make compose-up
make migrate
make migrate-status
make test-integration
make smoke-test SMOKE_URL=http://localhost:8080
```

## Уровень D — контейнерная сборка

```bash
docker compose build
docker compose up -d
./scripts/smoke-test.sh http://localhost:8080
```

Проверяется: минимальные runtime-образы, запуск без root, health checks, корректная
конфигурация через окружение, отсутствие скачиваний в рантайме.

## Уровень E — чистое развёртывание

```bash
make compose-reset         # осознанно: удаляет данные
make compose-up
make migrate               # миграции на пустой БД
make smoke-test
make backup
make restore DUMP=<путь>   # проверка, что копия восстанавливается
make smoke-test            # сервис работает после восстановления
```

Этот цикл автоматизирован в `.github/workflows/portability.yml` (jobs `upgrade-path`,
`backup-restore`, `compose-smoke`).

## Переменные окружения

Полный перечень — `.env.example`. Сборка **не требует секретов**: `make build`,
`make test-unit`, `make openapi-check` и `make docs-check` работают без `.env`.
Секреты нужны только интеграционным уровням и развёртыванию.

## Кэширование

- Go: модульный кэш (`GOCACHE`, `GOMODCACHE`) — годится для CI.
- Frontend: `npm ci` с кэшем `~/.npm`; `node_modules` не коммитится.
- Docker: BuildKit кэш слоёв; порядок `COPY` — сначала манифесты, потом исходники.

## Типовые ошибки

| Ошибка | Причина | Решение |
| --- | --- | --- |
| `go: command not found` | нет Go или не в `PATH` | `make doctor` покажет; версия должна быть 1.26.10 |
| `go.mod requires go >= 1.26.10` | локально старее | обновить toolchain, не понижать `go.mod` |
| `npm ci` падает | нет lockfile-синхронизации | `npm install` не использовать в CI; править `package.json` + lockfile |
| `openapi-gen -check` падает | маршрут изменён без регенерации | `make openapi` |
| миграции падают на повторном запуске | неидемпотентная миграция | исправить новой миграцией |
| E2E не находит браузер | Playwright не установлен | `npx playwright install --with-deps chromium` |
| compose smoke не проходит | стек не healthy | `make compose-logs`, `docker compose ps` |

## Что считается доказательством сборки

Не «у меня собралось», а зелёный прогон в чистом CI-окружении: `ci.yml` (уровни A–B,
C для интеграционных тестов) и `portability.yml` (уровни D–E). Матрица утверждений —
[docs/verification/matrix.md](docs/verification/matrix.md).
