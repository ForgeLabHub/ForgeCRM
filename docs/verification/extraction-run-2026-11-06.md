# Прогон набора выделения — 2026-11-06

Записанное доказательство одного полного прогона `scripts/extract-test/run.sh`.
`docs/EXTRACTION_TESTING.md` требует, чтобы прошедшее выделение было записано
(шаг 11): «выделение, которое прошло, но не было записано, не считается».
Записи в `docs/EXTRACT_READY.md` привязаны к исходному коммиту `7a6d67f`; этот
документ фиксирует прогон на текущем `main`, где модулей стало тридцать шесть и
появились Telegram, оформление и миграции 0030–0036.

## Начальное состояние (проверено, не предположено)

| Факт | Значение |
| --- | --- |
| Ветка | `main` |
| HEAD | `9bd0f71ee1325dfb6f69fae65716f5eb2c211924` |
| `origin/main` | `9bd0f71ee1325dfb6f69fae65716f5eb2c211924` (совпадает) |
| Рабочее дерево | чистое (`git status --porcelain` пуст) |
| Инструмент | `go version go1.26.10 linux/amd64`, `GOTOOLCHAIN=local` |
| Набор | `scripts/extract-test/modules.txt`, 36 модулей |
| Задание CI | `.github/workflows/ci.yml` → `extraction` (строка 131) |

## Команда

```bash
export PATH=/workspace/go-sdk/go/bin:$PATH
export GOTOOLCHAIN=local
bash scripts/extract-test/run.sh
```

## Результат

```
== result ==
   ok    every module extracted cleanly
```

**36 из 36 модулей выделились чисто.** Ни одного `FAIL`, ни одного пропуска
(`skipping`) и ни одного отсутствующего `setup.sh`. Для каждого модуля набор
скопировал только файлы пакета (и объявленные зависимости) в чистый модуль Go,
затем выполнил `assert_no_monolith_import`, `go build ./...`, `go test -count=1
./...` и `go run ./cmd/smoke` — 36 запусков smoke, по одному на модуль.

## Модули (в порядке `modules.txt`)

| # | Модуль | # | Модуль |
| ---: | --- | ---: | --- |
| 1 | `netguard` | 19 | `integrations` |
| 2 | `analytics` | 20 | `config` |
| 3 | `ratelimit` | 21 | `workflow` |
| 4 | `objectstore` | 22 | `events` |
| 5 | `secrets` | 23 | `migratex` |
| 6 | `logging` | 24 | `simulate` |
| 7 | `httpx` | 25 | `forecast` |
| 8 | `importx/safe` | 26 | `risk` |
| 9 | `importx/parsers` | 27 | `notify` |
| 10 | `database` | 28 | `ai` |
| 11 | `rbac` | 29 | `importx` |
| 12 | `audit` | 30 | `migrations` |
| 13 | `outbox` | 31 | `migrate` |
| 14 | `retention` | 32 | `authn` |
| 15 | `agent` | 33 | `oidc` |
| 16 | `jobs` | 34 | `views` |
| 17 | `crm` | 35 | `constructor` |
| 18 | `storage` | 36 | `tenantconfig` |

## Выборочные выводы smoke (доказательство, что путь кода исполнялся)

Smoke каждой модуля утверждает наблюдаемое поведение, а не только компиляцию:

```
netguard     loopback blocked=1 metadata blocked=1 public allowed=1
analytics    funnel_metrics=4 revenue_metrics=3
ratelimit    allowed=3 limit=3
objectstore  roundtrip=16 listed=1
secrets      password verified=1 mismatch detected=1 keyring roundtrip=1
logging      correlation roundtrip=1 secret redacted=1 benign kept=1
importx/safe roundtrip files=1 bytes=9 / traversal refused=1 / zip bomb refused=1
importx/parsers csv columns=[name email column_3 email_2] rows=2 errors=1
database     default=16 allowed=25
rbac         catalog=103 / principal perms=2 / owner perms=103
```

## Дешёвая половина: archtest

Набор — дорогая половина проверки; дешёвая — `TestExtractionBoundaryHolds` в
`internal/archtest`, которая применяет ту же границу из графа импортов на каждом
`go test ./...`. Прогнано на том же коммите:

```
ok  github.com/geff1778/forgecrm/internal/archtest  4.786s
```

## Что этот документ не утверждает

- Он не предлагает и не выполняет никакого выделения модулей. Прогон набора —
  это проверка свойства, а не решение о рефакторинге.
- Он не переписывает `docs/EXTRACT_READY.md`. Тот документ остаётся записью
  исходного среза `7a6d67f`; здесь зафиксирован более поздний прогон на
  `9bd0f71`.
- Он не касается воркстрима BOLA/IDOR (Agent Security); тот уже влит
  (`f67c632`, предок `HEAD`).

## Воспроизведение

```bash
cd backend && export PATH=/workspace/go-sdk/go/bin:$PATH && export GOTOOLCHAIN=local
go test -count=1 ./internal/archtest/
cd .. && bash scripts/extract-test/run.sh
```

Порядок и охват набора описаны в `docs/EXTRACTION_TESTING.md`; как добавить
модуль — в `scripts/extract-test/README.md`.
