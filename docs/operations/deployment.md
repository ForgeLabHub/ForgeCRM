# Развёртывание

## Поддерживаемая топология

| Топология | Файл | Когда |
| --- | --- | --- |
| Один хост, один экземпляр | `docker-compose.yml` | по умолчанию, рекомендуется |
| Несколько реплик API | `docker-compose.multireplica.yml` + `deploy/nginx-multireplica.conf` | при нагрузке; требует Redis и внешнего прокси |
| Без контейнеров | статические бинарники + внешний PostgreSQL | если политика запрещает контейнеры |

## Порядок первого развёртывания

```bash
cp .env.example .env
printf 'FORGECRM_SECRET_KEY=%s\n' "$(openssl rand -base64 48)" >> .env
make doctor            # проверка хоста, ничего не меняет
make compose-up
make migrate
make smoke-test SMOKE_URL=http://localhost:8080
```

Создание первого администратора — через переменную окружения, чтобы пароль не
попадал в историю шелла:

```bash
FORGECRM_BOOTSTRAP_PASSWORD='<strong>' \
  docker compose run --rm --no-deps -e FORGECRM_BOOTSTRAP_PASSWORD \
  api -bootstrap-admin -bootstrap-email you@example.com
```

`scripts/setup.sh you@example.com` делает то же самое одной командой.

## Обязательные к решению вопросы перед продом

1. **Секреты.** `FORGECRM_SECRET_KEY` — вне репозитория и вне дампа; потеря означает
   невозможность расшифровать сохранённые секреты интеграций.
2. **TLS.** Терминируется nginx или внешним прокси; `FORGECRM_COOKIE_SECURE=true`.
3. **Прокси.** `FORGECRM_TRUSTED_PROXIES` перечисляет доверенные адреса — иначе
   ограничение по IP обходится подделкой `X-Forwarded-For`.
4. **Резервные копии.** Расписание и проверенное восстановление, см.
   [database/backup-restore.md](../database/backup-restore.md).
5. **Ресурсы.** Лимиты памяти PostgreSQL и пула (`FORGECRM_DATABASE_MAX_CONNS`)
   должны согласовываться, иначе соединения закончатся под нагрузкой.
6. **Метрики.** `FORGECRM_METRICS_TOKEN` задан, если `/metrics` доступен не только
   из локальной сети.

## Проверка после развёртывания

- `make smoke-test` — сервис жив и отвечает.
- `scripts/smoke-test.sh` с кредами — проверяет вход и чтение данных.
- Health endpoints в [monitoring.md](monitoring.md).
- `scripts/multi-replica-verify.sh` — для многокластерной топологии.
