# Первый запуск для разработчика

## Требования

| Инструмент | Версия | Зачем |
| --- | --- | --- |
| Go | 1.26.10 (как в `backend/go.mod`) | backend |
| Node.js | 22.x | frontend |
| Docker + Compose plugin | любой актуальный | PostgreSQL, Redis, полный стек |
| make, git, bash | — | цели Makefile |

Проверка окружения (ничего не устанавливает и не меняет):

```bash
make doctor
```

## Два пути

### A. Полный стек в контейнерах (рекомендуется)

```bash
make setup ADMIN_EMAIL=you@example.com
make smoke-test SMOKE_URL=http://localhost:8080
```

`make setup` создаёт `.env`, генерирует `FORGECRM_SECRET_KEY`, поднимает базу,
применяет миграции и создаёт администратора. Повторный запуск безопасен: он не
перезаписывает `.env` и не создаёт второго администратора.

### B. Локально, без контейнеров для приложения

```bash
cp .env.example .env
# указать FORGECRM_DATABASE_URL на локальный PostgreSQL
make migrate
make build
./bin/forgecrm-api          # API
./bin/forgecrm-worker       # worker, отдельный терминал
cd frontend && npm ci && npm run dev
```

## Переменные окружения

Полный список — `.env.example` (более 70 параметров `FORGECRM_*`). Ни один из них
не является секретом в репозитории; секреты задаются только в `.env`, который не
коммитится.

Три, которые ломают больше всего:

| Переменная | Ошибка | Симптом |
| --- | --- | --- |
| `FORGECRM_SECRET_KEY` | забыли/сменили | секреты интеграций не расшифровываются |
| `FORGECRM_DATABASE_URL` | неверный DSN | API не стартует с внятным сообщением |
| `FORGECRM_COOKIE_SECURE=true` без TLS | вход не работает | браузер не отправляет cookie |

Конфигурация валидируется при старте: несовместимые комбинации (например
`FORGECRM_REDIS_URL` при `FORGECRM_RATE_LIMIT_STORE=memory`) отвергаются, а не
игнорируются.

## Что делать дальше

- Проверки — [testing.md](testing.md).
- Стиль — [code-style.md](code-style.md).
- Отладка — [debugging.md](debugging.md).
