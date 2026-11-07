# Модуль: parsers

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package parsers converts tabular source files into a common row model.

## 3. Контракт
Публичный Go-API:
- `func ParseBool`
- `func ParseDate`
- `func ParseNumber`
- `type Batch`
- `type Format`
- `type Options`
- `type Result`
- `type Row`
- `type RowError`
- `type Sheet`
- `type StreamCallback`
- `type StreamOptions`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: нет (лист).

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/importx/parsers
go test ./internal/importx/parsers/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh parsers   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/parsers` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
