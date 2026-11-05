# Тестирование

## Уровни

| Уровень | Команда | Требует | Что доказывает |
| --- | --- | --- | --- |
| Unit (backend) | `make test-unit` | Go | бизнес-правила живут без БД |
| Unit (frontend) | `make test-frontend` | Node | компоненты и клиенты |
| Architecture | `go test ./internal/archtest/` | Go | правила зависимостей не нарушены |
| Contract/OpenAPI | `make openapi-check` | Go | спецификация не разошлась с роутером |
| Integration | `make test-integration` | PostgreSQL | транзакции, RBAC, изоляция арендаторов |
| Benchmarks (baseline) | `make test-bench` | PostgreSQL (черновая БД) | записанное измерение, не гейт; см. [performance/baseline.md](../performance/baseline.md) |
| E2E | `make test-e2e` | PostgreSQL + Chromium | пользовательские сценарии в браузере |
| Визуальная регрессия | `make test-visual` | PostgreSQL + Chromium (образ Playwright) | макет и токены не смещаются |
| Документация | `make docs-check` | — | документы существуют, ссылки живые, спецификации модулей полные |

## Правила

1. **Тесты не подгоняются под результат.** Если тест падает, сначала выясняется,
   что неверно — код или утверждение.
2. **Unit-тесты не открывают БД.** Бизнес-логика, требующая PostgreSQL для проверки,
   проверяется интеграционным тестом, а не «мок-базой».
3. **Интеграционные тесты помечены** `//go:build integration` (70 файлов на текущем
   `main`) и запускаются
   только с `-tags=integration` и явным `FORGECRM_TEST_DATABASE_URL`. Случайный запуск
   против рабочей базы невозможен.
4. **Каждый пакет заводит свою базу.** `internal/testdb` выводит из
   `FORGECRM_TEST_DATABASE_URL` приватную базу `forgecrm_test_<пакет>_<pid>`, поэтому
   пакеты не делят одну схему и идут параллельно. Раньше все они сбрасывали общую
   `public`, и прогон требовал `-p 1`, иначе один пакет ронял таблицы другого.
5. **Каждый дефект безопасности получает регрессионный тест** — иначе он вернётся.
6. **Утверждение в документации ищет себе тест.** См.
   [verification/matrix.md](../verification/matrix.md).

## Запуск против реальной базы

```bash
make compose-up
make migrate
make test-integration TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable'
```

Тестовая база должна быть отдельной: интеграционные тесты создают и удаляют данные.

### Локальное окружение, совпадающее с CI

Чтобы локальный прогон был неотличим от джобы `backend` в CI, есть
`scripts/integration-env.sh`: он поднимает `postgres:17-alpine` и
`redis:7-alpine` — те же образы, что в CI, но на отдельных портах (55432/56379),
чтобы не трогать рабочий стек.

```bash
./scripts/integration-env.sh up
export FORGECRM_TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:55432/forgecrm_test?sslmode=disable'
export FORGECRM_TEST_REDIS_URL='redis://127.0.0.1:56379/1'
make test-integration TEST_DATABASE_URL="$FORGECRM_TEST_DATABASE_URL"
./scripts/integration-env.sh down
```

### Фикстуры

`internal/testfixture` (test-only) собирает арендатора, пользователей с
резолвнутыми правами и связный граф домена, чтобы тест не писал SQL для
подготовки. Подробнее — [quality/TEST_ARCHITECTURE.md](../quality/TEST_ARCHITECTURE.md).

## E2E

```bash
./scripts/e2e-bootstrap.sh     # подготовка владельца платформы
make test-e2e                  # Playwright, Chromium
```

## Визуальная регрессия

```bash
./scripts/e2e-bootstrap.sh     # тот же стек, что и для E2E
make test-visual               # сравнение с эталонами в frontend/e2e/visual.spec.ts-snapshots
```

Набор намеренно узкий: по одному экрану на каждый визуальный примитив (оболочка, тёмная
тема, таблица, карточки на телефоне, диалог, плитки отчёта, страница входа и её
русский вариант), а не по одному на маршрут. Динамические значения — числа, даты, id,
инициалы — маскируются точечно, чтобы в сравнении оставался макет, а не данные прогона.

Эталоны — **только для Linux**. Они сгенерированы и сравниваются внутри закреплённого
образа `mcr.microsoft.com/playwright:v1.49.1-jammy` — того же, что использует джоба
`visual` в CI, — поэтому шрифт и растеризация совпадают с обеих сторон. Прогон на другой
платформе разойдётся только из-за сглаживания шрифтов и ему доверять нельзя. Обновлять
эталоны нужно в этом образе:

```bash
docker run --rm --network host -e FORGECRM_API_URL=http://127.0.0.1:8080 \
  -v "$PWD/frontend":/work -w /work mcr.microsoft.com/playwright:v1.49.1-jammy \
  npx playwright test --project=visual --update-snapshots
```

## Чего в наборе нет

- Нагрузочных тестов под конкуренцией (есть `scripts/multi-replica-verify.sh` для
  проверки корректности нескольких реплик, но не производительности, и
  `make test-bench` — однопоточная базовая линия, а не нагрузка).
- Property-based тестов домена, кроме точечных случаев.
- Мутационного тестирования.
