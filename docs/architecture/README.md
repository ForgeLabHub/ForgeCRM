# Архитектурная документация ForgeCRM

Навигация по архитектурным документам. Всё описанное ниже проверено против кода
на коммите `main` @ `3424b3e`; там, где документ описывает **целевое**, а не
текущее состояние, это прямо помечено.

## Что читать первым

| Вопрос | Документ |
|---|---|
| Что такое ForgeCRM сейчас (факт) | [`SYSTEM_OVERVIEW.md`](SYSTEM_OVERVIEW.md) |
| Что такое модульный монолит и почему `main` — это он | [`MODULAR_MONOLITH.md`](MODULAR_MONOLITH.md) |
| Целевая микросервисная архитектура и её статус | [`MICROSERVICES.md`](MICROSERVICES.md) |
| Как переносить модуль в другой проект | [`PORTABLE_MODULES.md`](PORTABLE_MODULES.md) |
| Результат независимого аудита | [`audit/PROJECT_AUDIT.md`](audit/PROJECT_AUDIT.md) |
| Реестр технического долга | [`audit/TECHNICAL_DEBT.md`](audit/TECHNICAL_DEBT.md) |
| Аудит безопасности | [`audit/SECURITY_AUDIT.md`](audit/SECURITY_AUDIT.md) |
| Граф зависимостей и владение данными | [`audit/DEPENDENCY_MAP.md`](audit/DEPENDENCY_MAP.md) |
| Итоговый отчёт программы | [`FINAL_ARCHITECTURE_REPORT.md`](FINAL_ARCHITECTURE_REPORT.md) |

## Действующие архитектурные решения (ADR)

Полный список — в [`docs/adr/`](../adr/README.md). Ключевые:

- **ADR 0001** — гибридная архитектура: один модуль, два процесса, границы
  обеспечиваются направлением зависимостей. **Действует.**
- **ADR 0002** — измеримые триггеры выделения сервиса (T1…). **Действует.**
- **ADR 0013** — статус микросервисной реализации относительно ADR 0001/0002.
  **Предложено** (требует решения владельца).

## Другие архитектурные документы репозитория

- [`../ARCHITECTURE.md`](../ARCHITECTURE.md) — целевая архитектура и принципы.
- [`../MODULES.md`](../MODULES.md) — инвентаризация модулей по коду.
- [`../EXTRACT_READY.md`](../EXTRACT_READY.md) — готовность модулей к выделению.
- [`../DEPENDENCY_MAP.md`](../DEPENDENCY_MAP.md) — историческая карта зависимостей.
- [`../MODULAR_MONOLITH.md`] — см. локальный файл в этом каталоге.
- [`modules/`](../modules/) — спецификации отдельных модулей (11 разделов).
- [`../adr/`](../adr/) — архитектурные решения.
- [`../verification/project-facts.md`](../verification/project-facts.md) —
  машинно-проверяемые факты (гейт `make facts-check`).

## Как это проверяется

| Утверждение | Гейт |
|---|---|
| Модули извлекаются | `scripts/extract-test/run.sh` (CI job `extraction`) |
| Нет циклов, правила слоёв держатся | `go test ./internal/archtest/...` |
| Факты совпадают с кодом | `make facts-check` (CI job `docs`) |
| OpenAPI актуален | `make openapi-check` |
| Спецификации модулей полны | `scripts/docs-check.sh` |
| Манифесты переносимости | `scripts/module-docs-check.sh` |

## Диаграммы

Mermaid-диаграммы встроены в `SYSTEM_OVERVIEW.md` (общая архитектура, потоки
запросов и событий), `MODULAR_MONOLITH.md` (модули и зависимости),
`MICROSERVICES.md` (целевые контейнеры и потоки) и
`audit/DEPENDENCY_MAP.md` (граф зависимостей).
