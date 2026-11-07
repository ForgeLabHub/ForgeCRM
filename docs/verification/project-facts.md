# Факты о проекте

Машинно-проверяемая сводка размеров и согласованности репозитория. Блок между
маркерами `FACTS:AUTO BEGIN` и `FACTS:AUTO END` генерируется из исходного кода
(`backend/cmd/facts-gen`): числа извлекаются из файлов, а не поддерживаются
вручную, поэтому документ не может тихо разойтись с кодом.

## Как обновить

```
make facts          # перегенерировать блок в этом файле на месте
make facts-check    # упасть, если блок разошёлся с кодом (то же, что в CI)
```

Эквивалент без Makefile:

```
cd backend
go run ./cmd/facts-gen -repo .. -write    # обновить блок, сохранив текст вокруг
go run ./cmd/facts-gen -repo ..           # напечатать блок в stdout
go run ./cmd/facts-gen -repo .. -check    # проверить, ничего не меняя
```

`-write` заменяет только регион между маркерами и не трогает написанный вручную
текст вокруг. CI запускает `-check` и падает, когда блок разошёлся с кодом или
когда независимые источники противоречат друг другу.

## Что проверяется автоматически

- константы RBAC совпадают с каталогом разрешений;
- засеянные в БД разрешения совпадают с каталогом (`platform.owner` исключён:
  владелец платформы держит права через членство в роли, а не через код);
- число up- и down-миграций симметрично;
- пути маршрутов `App.tsx` уникальны внутри каждого дерева `<Routes>`
  (повтор между анонимным и аутентифицированным деревьями допустим — это
  взаимоисключающие ветви, а не мёртвая маршрутизация).

Числа ниже — измеренное состояние, а не цель. Расхождение с кодом означает, что
код изменился, а документ не перегенерировали.

<!-- FACTS:AUTO BEGIN — генерируется backend/cmd/facts-gen, не редактировать вручную -->
<!-- измерено на HEAD `3b2de2dc079414ff652a51b35543ef1b2e48bde5` -->

## Backend / Architecture

| Метрика | Значение | Определение |
|---|---|---|
| Пакеты Go (весь backend) | **57** | каталоги с хотя бы одним .go-файлом под backend/ |
| Пакеты Go (backend/internal) | **52** | то же, ограничено backend/internal/** |
| Go-файлы без тестов | **162** | *.go, кроме *_test.go |
| Go тестовых файлов | **163** | *_test.go |
| Тестовые функции Go | **974** | верхнеуровневые func TestXxx(*testing.T), TestMain исключён |
| Бенчмарки Go | **3** | func BenchmarkXxx |
| Fuzz-тесты Go | **0** | func FuzzXxx |
| Example-тесты Go | **0** | func ExampleXxx |

## Database

| Метрика | Значение | Определение |
|---|---|---|
| Миграций up | **37** | *.up.sql в backend/internal/migrations |
| Миграций down | **37** | *.down.sql там же |
| Таблиц БД | **83** | уникальные имена CREATE TABLE во всех up-миграциях |

## Security (RBAC)

| Метрика | Значение | Определение |
|---|---|---|
| Константы Permission | **103** | строковые константы типа Permission в internal/rbac/rbac.go |
| Записи каталога | **103** | элементы map catalog в rbac.go |
| Разрешений в БД (seeded) | **103** | уникальные коды из INSERT INTO permissions во всех up-миграциях |
| Системных ролей | **14** | кортежи INSERT INTO roles с is_system = true |

Каталог и сид совпадают точно: наборы равны, симметричной разницы нет.
`platform.owner` генератор всё равно исключает из сравнения сида на будущее:
владелец платформы держит права через членство в роли, а не через отдельный код
разрешения, поэтому появление такого кода в сиде не должно считаться каталогом.

## API

| Метрика | Значение | Определение |
|---|---|---|
| OpenAPI paths | **183** | ключи objects.paths в docs/api/openapi.json |
| OpenAPI operations | **238** | методы get/post/put/patch/delete внутри paths |

openapi.json генерируется из живого роутера (`cmd/openapi-gen`), поэтому
счётчик отражает код, а CI-гейт `openapi-check` не даёт документу устареть.

## Frontend

| Метрика | Значение | Определение |
|---|---|---|
| Файлы src (.ts/.tsx) | **148** | frontend/src/**, включая тестовые |
| Тестовые файлы src | **48** | *.test.ts(x)/.spec.ts(x) в frontend/src |
| Объявления it()/test() в src | **312** | запускаемые кейсы vitest; test.describe вычитается |
| Файлы страниц | **39** | компоненты в frontend/src/pages (без admin/, без тестов) |
| Тесты страниц | **21** | *.test.tsx рядом со страницами |
| Маршрутов <Route path> | **47** | объявления в frontend/src/App.tsx (регистр маршрутизации SPA) |
| Уникальных путей маршрутов | **47** | различающиеся path по всем <Routes>-деревьям App.tsx; повторения внутри одного дерева запрещены (гейт route_paths_unique) |
| E2E спецификации | **8** | *.spec.ts в frontend/e2e |
| E2E тесты | **82** | объявления test() Playwright; test.describe вычитается |

## CI/CD

| Метрика | Значение | Определение |
|---|---|---|
| Workflow-файлов | **2** | *.yml/*.yaml в .github/workflows |
| Jobs суммарно | **16** | ключи двухпробельного отступа внутри секции jobs: |

Список заданий: ci.yml/backend, ci.yml/blitz-cloud, ci.yml/constructor, ci.yml/docker, ci.yml/docs, ci.yml/e2e, ci.yml/extraction, ci.yml/frontend, ci.yml/security, ci.yml/visual, portability.yml/backup-restore, portability.yml/compose-smoke, portability.yml/config-without-secrets, portability.yml/docs, portability.yml/local-start-smoke, portability.yml/upgrade-path.

## Consistency flags

| Проверка | Результат |
|---|---|
| константы RBAC == каталог | ✅ ok |
| seeded разрешения == каталог (кроме platform.owner) | ✅ ok |
| миграции up/down симметричны | ✅ ok |
| маршруты App.tsx уникальны | ✅ ok |

<!-- FACTS:AUTO END -->
