# Тестирование

## Уровни

| Уровень | Команда | Требует | Что доказывает |
| --- | --- | --- | --- |
| Unit (backend) | `make test-unit` | Go | бизнес-правила живут без БД |
| Unit (frontend) | `make test-frontend` | Node | компоненты и клиенты |
| Architecture | `go test ./internal/archtest/` | Go | правила зависимостей не нарушены |
| Contract/OpenAPI | `make openapi-check` | Go | спецификация не разошлась с роутером |
| Integration | `make test-integration` | PostgreSQL | транзакции, RBAC, изоляция арендаторов |
| E2E | `make test-e2e` | PostgreSQL + Chromium | пользовательские сценарии в браузере |
| Документация | `make docs-check` | — | документы существуют, ссылки живые, спецификации модулей полные |

## Правила

1. **Тесты не подгоняются под результат.** Если тест падает, сначала выясняется,
   что неверно — код или утверждение.
2. **Unit-тесты не открывают БД.** Бизнес-логика, требующая PostgreSQL для проверки,
   проверяется интеграционным тестом, а не «мок-базой».
3. **Интеграционные тесты помечены** `//go:build integration` (26 файлов) и запускаются
   только с `-tags=integration` и явным `FORGECRM_TEST_DATABASE_URL`. Случайный запуск
   против рабочей базы невозможен.
4. **Каждый дефект безопасности получает регрессионный тест** — иначе он вернётся.
5. **Утверждение в документации ищет себе тест.** См.
   [verification/matrix.md](../verification/matrix.md).

## Запуск против реальной базы

```bash
make compose-up
make migrate
make test-integration TEST_DATABASE_URL='postgres://forgecrm:forgecrm@127.0.0.1:5432/forgecrm_test?sslmode=disable'
```

Тестовая база должна быть отдельной: интеграционные тесты создают и удаляют данные.

## E2E

```bash
./scripts/e2e-bootstrap.sh     # подготовка владельца платформы
make test-e2e                  # Playwright, Chromium
```

## Чего в наборе нет

- Нагрузочных тестов (есть `scripts/multi-replica-verify.sh` для проверки
  корректности нескольких реплик, но не производительности).
- Property-based тестов домена, кроме точечных случаев.
- Мутационного тестирования.
