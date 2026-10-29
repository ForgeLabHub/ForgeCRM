# Модуль: Import Center

## 1. Назначение и границы
Перенос данных из внешних систем: определение формата, безопасный разбор,
сопоставление колонок, пакетное выполнение, отчёт по ошибкам строк.
Не делает: не изменяет справочники вне целевых сущностей и не «додумывает» данные —
неуверенное сопоставление требует решения человека.

## 2. Сценарии использования
- Загрузка CSV/TSV/JSON/JSONL/XLSX/XML и архивов; предпросмотр разбора.
- Маппинг колонок на поля CRM, сохранение шаблона.
- Запуск импорта с прогрессом; список ошибок по строкам.
- Повторный импорт с тем же файлом: дубли не создаются по правилам сопоставления.

## 3. Контракт
`/api/v1/imports/*` (загрузка, предпросмотр, шаблоны, запуск, отчёт). Публичный
контракт — `internal/importx`: `Service`, `parsers`, `writer`. События: `import.run`.

## 4. Данные
Владеет: `import_jobs`, `import_templates`, `import_row_errors`, `export_jobs`.
Пишет в целевые сущности **через контракт CRM**, а не прямыми `INSERT` в чужие
таблицы. Файлы — во временном каталоге `FORGECRM_IMPORT_TEMP_DIR` и томе хранения.

## 5. Права
`imports.*` (4 права), `exports.*` (2). Права на создание целевых записей
проверяются дополнительно — импорт не является обходом RBAC.

## 6. Зависимости
Исходящие: `crm` (контракт создания сущностей), `jobs`, `storage`, `audit`.
Входящие: `api`, `worker`.
Внешних систем нет: модуль не ходит в сеть.

## 7. Конфигурация
`FORGECRM_IMPORT_MAX_UPLOAD_BYTES`, `FORGECRM_IMPORT_MAX_UNPACKED_BYTES`,
`FORGECRM_IMPORT_MAX_ARCHIVE_DEPTH`, `FORGECRM_IMPORT_MAX_ARCHIVE_FILES`,
`FORGECRM_IMPORT_MAX_RATIO`, `FORGECRM_IMPORT_BATCH_SIZE`, `FORGECRM_IMPORT_TEMP_DIR`,
`FORGECRM_IMPORT_STORAGE_DIR`. Значения ограничивают ущерб от вредоносных файлов;
увеличение — осознанное решение с оценкой памяти.

## 8. Сборка и отладка
`make build-api`, отдельного бинарника нет. Локальная проверка разбора:

```bash
cd backend && go test ./internal/importx/... -v
```

## 9. Тесты
Unit: `internal/importx/format_detection_test.go`, `parsers/*_test.go`,
`safe/safe_test.go`, `safe/safe_formats_test.go`, `cleanup_test.go` (защита от
zip-bomb, path traversal, лимитов). Integration: `internal/api/import_handlers`-тесты,
`internal/testsupport/integration_test.go`. E2E: `ImportCenterPage` в
`frontend/e2e/platform.spec.ts`.

## 10. Наблюдаемость и ошибки
Прогресс и счётчики — `import_jobs`; ошибки строк — `import_row_errors`; аудит —
`audit_log`. Коды ошибок: `unsupported_format`, `mapping_required`, `limit_exceeded`,
`row_error` (по строкам, не прерывает импорт целиком).

## 11. Ограничения и готовность
- Нет автоматического распознавания «своей» схемы источника — маппинг подтверждает
  человек.
- XLSX разбирается в память пакетами; очень большие книги требуют увеличения лимитов.
- Готовность: лимиты безопасности покрыты тестами; повторный импорт не создаёт
  дублей; отчёт по строкам доступен после завершения.
