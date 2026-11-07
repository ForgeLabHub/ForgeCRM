# Модуль: analytics

> Метаданные сгенерированы `scripts/gen-module-docs.py` из кода. Не править
> вручную факты; для изменений — перегенерировать.

## 1. Назначение и границы
Package analytics is the pure calculation core of the intelligence platform.

## 3. Контракт
Публичный Go-API:
- `func FloatPercent`
- `func PercentString`
- `func SortPeriods`
- `func SortSignals`
- `type AccountObservation`
- `type Baseline`
- `type Comparison`
- `type ConversionPeriodObservation`
- `type DealObservation`
- `type Evidence`
- `type Forecast`
- `type ForecastMethod`
- `type ForecastParams`
- `type ForecastPoint`
- `type ForecastQuality`
- `type FunnelParams`
- `type Kind`
- `type Metric`
- `type Money`
- `type PeriodPoint`

HTTP-маршруты, если есть, объявлены в `internal/api` (слой композиции) и отражены
в `docs/api/openapi.json`.

## 6. Зависимости
Внутренние: нет (лист).

## 7. Конфигурация
- модуль читает окружение через `internal/config` (прямых `FORGECRM_*` нет)

## 8. Сборка и запуск
```bash
cd backend
go build ./internal/analytics
go test ./internal/analytics/...
```

## 9. Перенос в другой проект
```bash
./scripts/extract-test/run.sh analytics   # доказывает извлечение
```
Затем скопируйте пакет и перечисленные выше зависимости, перепишите импорты
`github.com/geff1778/forgecrm/internal/analytics` на путь вашего проекта и примените нужные миграции.

## 11. Ограничения
Модуль живёт в одном Go-модуле ForgeCRM (ADR 0001); переносимость доказана
харнессом извлечения, а не отдельным `go.mod`.
