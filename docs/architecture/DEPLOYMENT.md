# Развёртывание ForgeCRM

> Описывает **реальные** артефакты развёртывания на `main` @ `3424b3e`.

## Локальный запуск (Docker Compose)

```bash
cp .env.example .env      # отредактируйте; секреты не коммитить
docker compose up -d --build
# первый администратор (пароль через env, не в списке процессов):
FORGECRM_BOOTSTRAP_PASSWORD='...' docker compose run --rm --no-deps \
  -e FORGECRM_BOOTSTRAP_PASSWORD api -bootstrap-admin -bootstrap-email you@example.com
```

- `docker-compose.yml`: postgres + api + worker (+ redis, nginx — профили).
- `docker-compose.multireplica.yml` — несколько реплик API.
- `docker-compose.local.yml` — локальная разработка.

## Образ

- Корневой `Dockerfile` собирает один образ: оба бинарника + статика фронтенда.
- `deploy/entrypoint.sh` читает `FORGECRM_ROLE`/первый аргумент и запускает
  api или worker.
- Платформы без `entrypoint` (напр. blitz.cloud) используют `command: worker`.

## Health и готовность

| Endpoint | Смысл |
|---|---|
| `/health` | liveness |
| `/ready` | readiness (включая БД) |

Healthcheck в compose: API `wget /health`, Postgres `pg_isready`, Redis `redis-cli ping`.

## Миграции

- Встроены в образ; применяются API при `FORGECRM_MIGRATE_ON_START=true` (по
  умолчанию false). Worker стартует после API (условие `service_healthy`).
- Изменение схемы требует **пересборки** образа, а не просто перезапуска.

## Конфигурация без секретов

- `.env.example` — пример; секретов не содержит.
- `render.yaml` — манифест Render (без секретов).
- CI job `portability.yml/config-without-secrets` проверяет запуск без секретов.

## Проверка

| Утверждение | Команда / job |
|---|---|
| Стек поднимается | `docker compose up -d` |
| Smoke | `scripts/smoke-test.sh` |
| Мультиреплика | `scripts/multi-replica-verify.sh` |
| Восстановление из бэкапа | `portability.yml/backup-restore` |
| Обновление версии | `portability.yml/upgrade-path` |
