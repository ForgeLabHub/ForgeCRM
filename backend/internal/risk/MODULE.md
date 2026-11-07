# Модуль: risk

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package risk implements the Risk half of the Forecasting & Risk Lab: scanning the organization's CRM for explainable risk conditions and keeping the signals an operator works through.

## 3. Контракт
Публичный Go-API:
- `type ListFilter`
- `type ScanResult`
- `type Service`
- `type Signal`
- `type Thresholds`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: `analytics`, `audit`, `crm`, `database`, `rbac`.

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/risk
go test ./internal/risk/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh risk   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/risk` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
