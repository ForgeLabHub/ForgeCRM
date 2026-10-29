# Правила зависимостей

Правила не декларируются, а **проверяются**: `backend/internal/archtest/arch_test.go`
читает реальный граф импортов через `go list` и падает при нарушении. Поэтому этот
документ не может разойтись с кодом — если разойдётся, CI покраснеет.

## Слои и направление стрелок

```
cmd/api, cmd/worker          композиция и транспорт
        │
        ▼
internal/wiring              единственное место сборки графа зависимостей
        │
        ├──▶ internal/api    HTTP-транспорт
        ├──▶ internal/ai     адаптер модели + MCP
        │
        ▼
прикладные сервисы: importx, migratex, ai, events, workflow, agent
        │
        ▼
internal/crm                 ядро: сущности, правила, права
        │
        ▼
internal/database, internal/config, internal/logging   (без доменных знаний)
```

Стрелки идут только вниз. Ядро `crm` не знает о том, кто его вызывает.

## Что именно проверяется

| Тест | Утверждение |
| --- | --- |
| `TestNoPackageImportsAPI` | только композиционный слой импортирует `api` |
| `TestProductionPackagesDoNotImportTestsupport` | `testsupport` не попадает в бинарники |
| `TestApplicationServicesDependOnTheCoreAndNotTheReverse` | `crm` не импортирует прикладные сервисы |
| `TestCoreDoesNotImportCompositionOrHTTP` | ядро не зависит от транспорта |
| `TestEveryInternalPackageIsReachableFromACommand` | нет мёртвых пакетов |

Тест `TestEveryInternalPackageIsReachableFromACommand` ловит обратную проблему:
пакет, который никто не вызывает, — это либо забытая функциональность, либо
мёртвый код.

## Порты (внутренние интерфейсы)

Бизнес-логика зависит от интерфейсов, а не от вендоров. Реализации подключаются в
`internal/wiring`.

| Порт | Файл | Заменяемое |
| --- | --- | --- |
| `ai.Provider` | `internal/ai/ai.go` | Ollama, OpenAI-совместимый, локальная модель |
| `storage.ObjectStore` | `internal/storage/storage.go` | S3-совместимое хранилище, файловая система, память |
| `agent.Executor` | `internal/agent/agent.go` | способ исполнения действий агента |
| `integrations.Adapter` | `internal/integrations/hub.go` | внешние системы |
| `ratelimit.Store` | `internal/ratelimit/ratelimit.go` | память процесса, Redis |
| `events.Poster` | `internal/events/delivery.go` | HTTP-доставка вебхуков, тестовый приёмник |
| `migratex.Source` | `internal/migratex/migratex.go` | источник переносимых данных |
| `database.Querier` | `internal/database/database.go` | пул, транзакция, тестовый двойник |

Почта (`notify.EmailService`) пока конкретный тип, а не интерфейс: единственная
реализация — SMTP, вторая не появилась. Абстракция без второго потребителя —
лишний слой; условие появления описано в
[ADR 0001](../adr/0001-hybrid-architecture.md).

## Цена нарушения

Цикл, добавленный сегодня ради удобства, обнаруживается тогда, когда нужно
выделить модуль в отдельный сервис — то есть когда обещание уже дано. Тест стоит
дешевле, чем сломанное обещание.
