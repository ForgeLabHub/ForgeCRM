# Документация ForgeCRM

Точка входа в документацию. Читать сверху вниз, если вы впервые в проекте;
иначе — по таблице.

## Первые 15 минут

| Шаг | Документ |
| --- | --- |
| Понять, что это за продукт и чего в нём нет | [README.md](../README.md) |
| Собрать с нуля | [BUILD.md](../BUILD.md) |
| Понять устройство | [docs/ARCHITECTURE.md](ARCHITECTURE.md), затем [architecture/system-context.md](architecture/system-context.md) |
| Запустить и проверить | [development/setup.md](development/setup.md), [verification/matrix.md](verification/matrix.md) |
| Понять состояние проекта и риски | [PROJECT_AUDIT.md](PROJECT_AUDIT.md), [audit/portability-audit-2026-10-29.md](audit/portability-audit-2026-10-29.md) |

## Карта документации

| Вопрос | Где ответ |
| --- | --- |
| Как система устроена и почему так | `ARCHITECTURE.md`, `architecture/`, `adr/` |
| Как собрать и проверить | `BUILD.md`, `development/` |
| Как устроены данные и миграции | `database/` |
| Какие есть API и контракты | `api/`, `docs/api/openapi.json` |
| Что делает конкретный модуль | `modules/` |
| Как эксплуатировать и обновлять | `operations/`, `deployment/` |
| Модель угроз, права, tenant isolation | `security/` |
| Чем доказано, что это работает | `verification/matrix.md`, CI |
| Сколько в проекте чего (проверяемые числа) | [verification/project-facts.md](verification/project-facts.md) |
| Что запланировано и что принято | `ROADMAP.md`, `ACCEPTANCE_CRITERIA.md`, `MASTER_PLAN.md` |
| Какие модули извлекаемы и на каком основании | [MODULES.md](MODULES.md), [EXTRACT_READY.md](EXTRACT_READY.md), [EXTRACTION_TESTING.md](EXTRACTION_TESTING.md) |
| Как собрать отдельный проект из выбранных модулей | [CONSTRUCTOR.md](CONSTRUCTOR.md) |
| Граф зависимостей и его проблемы | [DEPENDENCY_MAP.md](DEPENDENCY_MAP.md) |
| Что значит «модуль готов» | [ORG_DOD.md](ORG_DOD.md) |
| Как настроить demo / dev / hackathon / production | [CONFIG_PROFILES.md](CONFIG_PROFILES.md) |

## Правила

1. **Документ не считается правдой, пока его не подтверждает проверка.** Если здесь
   написано «модуль не меняет данные CRM», это должно ловиться тестом. Матрица
   [verification/matrix.md](verification/matrix.md) связывает утверждение с
   командой и заданием CI.
2. **`docs/api/openapi.json` генерируется**, а не пишется руками (`make openapi`).
3. **Новый модуль без спецификации по [modules/module-template.md](modules/module-template.md)
   не считается готовым** — проверяется в CI (`make docs-check`).
4. **Ссылки проверяются автоматически** — битая относительная ссылка валит сборку.
5. **Не дублируйте.** Исторические документы (`MASTER_PLAN.md`,
   `INTELLIGENCE_PLATFORM_PLAN.md`) сохранены как есть; этот индекс объясняет, что
   из них актуально, а что отражает прошлое состояние.

## Статус документов

| Документ | Состояние |
| --- | --- |
| `ARCHITECTURE.md`, `adr/*` | Актуально, поддерживается |
| `PROJECT_AUDIT.md`, `audit/*` | Снимок на дату, не переписывается задним числом |
| `MASTER_PLAN.md`, `INTELLIGENCE_PLATFORM_PLAN.md` | Историческое планирование; текущее состояние — в `PROJECT_AUDIT.md` |
| `ROADMAP.md`, `ACCEPTANCE_CRITERIA.md` | Живые документы |

## Управление проектом

- `forge.sh` / `forge.bat` — запуск, остановка, логи, тесты, база и контейнеры одной командой; см. раздел в README.
