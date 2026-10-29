# Модуль: интеграции и вебхуки

## 1. Назначение и границы
Управляемый обмен с внешними системами: исходящие вебхуки, адаптеры интеграций,
защита от SSRF, повторная доставка. Не делает: не даёт произвольный исходящий HTTP
из бизнес-логики — только через адаптер и проверку адреса.

## 2. Сценарии использования
- Подписка на события, доставка с подписью и повторами.
- Просмотр недоставленных, повтор (replay), разбор dead letter.
- Проверка соединения с интеграцией до её включения.
- Отказ: адрес указывает в приватную сеть → блокировка с объяснением.

## 3. Контракт
`/api/v1/integrations/*`, `/api/v1/webhooks/*`, `webhook_deliveries`.
Порты: `integrations.Adapter`, `events.Poster`. События наружу — подписанные.

## 4. Данные
Владеет: `integrations`, `integration_runs`, `webhook_subscriptions`,
`webhook_deliveries`, `events_inbox`, `outbox_events`.
Миграции: `0006`–`0008` (права, диспетчеризация, жизненный цикл доставки).

## 5. Права
`integrations.*`, `webhooks.manage`, `events.read`. Секреты интеграций доступны
только тем, у кого есть право управления.

## 6. Зависимости
Исходящие: `secrets` (шифрование секретов), `outbox`, `jobs`, `audit`, `httpx`.
Внешние системы — по протоколу: HTTP/SMTP/S3/AI-провайдер.

## 7. Конфигурация
Секреты — в БД, зашифрованы `FORGECRM_SECRET_KEY`. `FORGECRM_TRUSTED_PROXIES`
влияет на определение источника запроса. Ретраи и бюджет попыток —
[ADR 0004](../adr/0004-webhook-retry-budget-and-replay.md).

## 8. Сборка и отладка
Доставка — worker (`make build-worker`). Проверки:

```bash
cd backend && go test ./internal/integrations/ ./internal/events/ -v
```

## 9. Тесты
`internal/integrations/ssrf_test.go` (защита от SSRF),
`internal/api/webhook_lifecycle_test.go`, `webhook_replay_http_test.go`,
`internal/testsupport/webhook_deadletter_test.go`, `webhook_sink_test.go`.

## 10. Наблюдаемость и ошибки
`integration_runs` и `webhook_deliveries` — основной журнал; ошибки доставки видны
оператору, dead letter не удаляется автоматически. Ошибки: `blocked_address`,
`delivery_failed`, `signature_invalid`.

## 11. Ограничения и готовность
- Гарантия доставки — at-least-once; получатель должен быть идемпотентным.
- Нет экспоненциального бюджета бесконечно: после исчерпания — dead letter.
- Готовность: SSRF-проверки покрыты тестами; replay не создаёт дублей побочных
  эффектов; секреты не попадают в логи.
