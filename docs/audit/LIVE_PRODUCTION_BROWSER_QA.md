# Живой QA опубликованного ForgeCRM (браузер + API)

- **Дата и время проверки:** 2026-11-04, ~12:05–12:20 UTC
- **URL приложения:** `https://forgecrm.forgecrm.blitz.cloud/`
- **Проверяемый код (фактически развёрнутый):** `main` (`30d0446`) — подтверждено совпадением бандла
- **Заявленный `/ready` commit:** `8d350b0cbdbbc815cce3253a083b30cfd706fa15` (устаревший, см. D-1)
- **Версия:** `main` (переменная `FORGECRM_VERSION`)
- **Окружение:** Playwright 1.49.1 / Chromium 131.0.6778.33, curl
- **Итоговый статус:** **LIVE QA BLOCKED**

> Главная цель — проверка продукта под администратором — **не достигнута**:
> предоставленные учётные данные не проходят аутентификацию. Проверена публичная
> поверхность, аутентификация (негативно), защита маршрутов, безопасность,
> UI/i18n/темы и корректность артефактов развёртывания. Все авторизованные
> модули — `NOT TESTED`. PASS за непроверенное не выдан.

---

## 1. Доступность

Приложение **живо** (ранее отвечало `503 asleep`, теперь работает).

| Адрес | HTTP | Время | Тип | Redirects |
| --- | --- | --- | --- | --- |
| `/` | 200 | 0.49 s | text/html | 0 |
| `/login` | 200 | 0.43 s | text/html | 0 |
| `/health` | 200 | 0.43 s | application/json | 0 |
| `/ready` | 200 | 0.52 s | application/json | 0 |
| `/admin` | 200 | 0.17 s | text/html | 0 |
| `/admin/audit` | 200 | 0.42 s | text/html | 0 |
| `/nonexistent-xyz` | 200 | — | text/html | 0 (SPA-fallback) |

- `/health` → `{"service":"forgecrm","status":"ok","version":"main"}`
- `/ready` → `{"commit":"8d350b0…","status":"ready","version":"main"}`
- HTML корректен: `<title>ForgeCRM</title>`, подключены `/assets/index-0PkA-8AD.js`
  и `/assets/index-DkcWs5hq.css`. **Загрузка CSS/JS без ошибок.**
- Неизвестный SPA-путь отдаёт `index.html` (200) — корректное поведение SPA-fallback.
- Неизвестный API-путь отдаёт JSON `404` (не HTML) — корректно.

**Устаревший frontend bundle: НЕ обнаружен.** Хэши живого бандла
(`index-0PkA-8AD.js` / `index-DkcWs5hq.css`) **байт-в-байт совпадают** с локальной
сборкой `main` (`npm ci && npm run build`). Сборка `8d350b0` даёт другие хэши
(`index-DL8VvKHY.js` / `index-DMvtk5tU.css`) — значит, развёрнут именно код `main`,
а не `8d350b0`.

---

## 2. Авторизация — FAIL / BLOCKED

Учётные данные из задания: email `почта-admin@gmail.com`, пароль — предоставленный
владельцем (в отчёте и артефактах не сохраняется).

| # | Сценарий | Результат |
| --- | --- | --- |
| 1 | Открытие `/login` | **PASS** (форма: email, password, submit) |
| 2 | Валидация пустого email/пароля | **PASS** («Enter your email address.», «Enter your password.») |
| 3 | Валидация формата email | **PASS** («Enter a valid email address.») |
| 4 | Вход с некорректными данными | **PASS** (общий `401` «Invalid email or password.», без утечки деталей) |
| 5 | Вход с предоставленными данными | **FAIL** → `401 invalid email or password` |
| 6–13 | Redirect после входа, сессия, защищённые страницы, админ-маршруты, logout, повторный вход, истечение сессии | **BLOCKED** (нет сессии) |
| 14 | Защита маршрутов без авторизации | **PASS** (см. §3) |

**Проверенные email (по одному `401` каждый):** `почта-admin@gmail.com`,
`mail-admin@gmail.com`, `geff1778@gmail.com`. Все три — `401`. Значение email в
задании содержит кириллицу («почта») и выглядит как незаполненный плейсхолдер
(«почта» = «mail»), поэтому, вероятнее всего, указан неверный email, а не
сломанный вход.

Попытки остановлены намеренно: порог блокировки аккаунта —
`FORGECRM_LOCKOUT_THRESHOLD=8` (окно 15 мин). Перебор не выполнялся.

---

## 3. Защита маршрутов без авторизации — PASS

Проверены 36 маршрутов SPA (Playwright, 1440×900). **Все защищённые маршруты
редиректят на `/login`** — route guard работает на клиенте; данные не
загружаются, каждый запрос `/api/v1/auth/me` даёт `401`:

`/`, `/contacts`, `/accounts`, `/deals`, `/tasks`, `/leads`, `/tickets`,
`/imports`, `/integrations`, `/webhooks`, `/workflows`, `/events`, `/reports`,
`/forecast`, `/risk`, `/simulation`, `/approvals`, `/jobs`, `/migrations`,
`/notifications`, `/ai`, `/account`, `/forbidden`, `/admin/audit`, `/admin/users`,
`/admin/roles`, `/admin/security`, `/admin/telegram`, `/admin/email`, `/admin/sso`,
`/admin/mcp-credentials`, `/admin/organizations`, `/admin/teams`,
`/admin/configuration`, `/admin/consent`, `/admin/diagnostics`.

Итог: **36/36 → `/login`**. Утечек данных или частично отрисованных
админ-страниц не обнаружено.

---

## 4. Обнаруженные маршруты (карта приложения)

Из бандла и `App.tsx` (публично видимая карта; наполнение не проверялось — `NOT TESTED`):

**CRM:** `/contacts`, `/contacts/:id`, `/accounts`, `/accounts/:id`, `/deals`,
`/deals/:id`, `/tasks`, `/tasks/:id`, `/leads`, `/leads/:id`, `/tickets`,
`/tickets/:id`, `/forecast`, `/risk`, `/simulation`, `/reports`.
**Данные/операции:** `/imports`, `/imports/:id`, `/events`, `/jobs`,
`/migrations`, `/notifications`, `/approvals`, `/workflows`, `/integrations`,
`/webhooks`, `/ai`.
**Админ:** `/admin/audit`, `/admin/users`, `/admin/roles`, `/admin/security`,
`/admin/telegram`, `/admin/email`, `/admin/sso`, `/admin/mcp-credentials`,
`/admin/organizations`, `/admin/teams`, `/admin/configuration`, `/admin/consent`,
`/admin/diagnostics`.
**Прочее:** `/login`, `/account`, `/forbidden`, `*`.

---

## 5. Модули CRM (Contacts/Accounts/Deals/Tasks) — NOT TESTED

Список, карточка, поиск, фильтры, сортировка, пагинация, CRUD, канбан, смена
стадии, прогноз, связи, задачи — **не проверены**: без сессии страницы
редиректят на `/login`. Тестовые данные с меткой `LIVE-QA-2026` **не создавались**
(создание невозможно без авторизации).

## 6. Import Center / Export — NOT TESTED
## 7. Admin / Audit — NOT TESTED
## 8. Integrations (Telegram/Email/Webhooks/OAuth/MCP) — NOT TESTED
## 9. MCP (клиентский протокол) — NOT TESTED

Проверено только без авторизации: `POST /api/v1/mcp` → `401`,
`GET /api/v1/mcp` → `405`. `initialize`/`tools/list` недоступны без сессии.

---

## 10. API — частично PASS (без авторизации)

| Проверка | Результат |
| --- | --- |
| `/api/v1/contacts` без сессии | `401` + RFC 9457 (`urn:forgecrm:error:unauthorized`) |
| `/api/v1/audit`, `/api/v1/users` | `401` |
| `/api/v1/auth/me` | `404` без сессии (эндпоинт есть, но требует сессию) |
| `/api/v1/auth/oidc/providers` | `200 {"enabled":false,"providers":[]}` (SSO выключен) |
| Неизвестный API-путь | `404` JSON |
| Валидация: `POST /auth/login {}` | `422 validation_failed` с `details.email` |
| Ошибки без stack trace | **PASS** — только код/сообщение/request_id |
| Rate limit | **PASS** — `ratelimit-limit: 300`, remaining корректно убывает |
| OpenAPI (`/openapi.json`, `/api/v1/openapi.json`) | `200 text/html` / `404` — **публично не отдаётся** |

Авторизация, RBAC, схемы ответов и пагинация **не проверены** (`NOT TESTED`).

---

## 11. Безопасность — частично PASS

**Заголовки (HTTPS, Cloudflare):**
- `strict-transport-security: max-age=31536000; includeSubDomains` ✅
- `x-content-type-options: nosniff` ✅, `x-frame-options: SAMEORIGIN` ✅
- `cross-origin-opener-policy: same-origin` ✅
- `referrer-policy: same-origin` ✅, `permissions-policy: geolocation=(), microphone=(), camera=()` ✅
- `x-robots-tag: noindex` ✅
- `content-security-policy-report-only: default-src 'self'; …` ⚠️ (report-only, см. D-2)

**Проверено:**
- ✅ Нет доступа к API без сессии (`401` на contacts/audit/users/mcp).
- ✅ CSRF: `POST /auth/logout` без сессии/токена → `401`, не выполняется.
- ✅ Ошибки без внутренних деталей и stack traces.
- ✅ В бандле нет hardcoded секретов/токенов (regex-скан 477 КБ — пусто).
- ✅ Аутентификация на серверной HttpOnly-сессии, без токенов в URL.
- ✅ Rate limiting активен.
- ⚠️ Cookie-атрибуты (`HttpOnly`/`Secure`/`SameSite`) **не проверены**: сессию
  получить не удалось. (`NOT TESTED`.)
- ⚠️ RBAC, CSRF на реальных мутациях, изоляция организаций, `401/403` под ролью —
  `NOT TESTED`.

---

## 12. UI и адаптивность — PASS (страница входа)

| Вьюпорт | Тема | H-overflow | Язык |
| --- | --- | --- | --- |
| 1440×900 | Light | нет | EN/RU |
| 1366×768 | Light | нет | EN/RU |
| 390×844 | Light | нет | EN/RU |
| 390×844 | Dark | нет | EN/RU |
| 1440×900 | Dark | нет | EN/RU |

- Переключатель темы **Light/Dark/System** работает: клик по «Dark» меняет
  `data-theme` `light → dark`.
- Локализация: при выборе RU — «Вход в рабочее пространство», кнопка «Войти»,
  `<html lang="ru">`.
- Горизонтальной прокрутки и наложения элементов не выявлено.

**Скриншоты:** `docs/audit/live-qa/01-login-desktop.png`, `02-login-empty-validation.png`,
`03-login-bad-creds.png`, `10-login-1440-light.png`, `11-login-1366-light.png`,
`12-login-390-light.png`, `13-login-390-dark.png`, `14-login-1440-dark.png`,
`15-login-dark-toggled.png`, `16-login-ru.png`.

---

## 13. Консоль и сеть

**Console errors (страница входа):**
- `[Report Only] Refused to load the script 'https://static.cloudflareinsights.com/beacon.min.js/...'` — Cloudflare-инъекция против report-only CSP. **Ожидаемо/безобидно** (report-only не блокирует), но создаёт шум.
- `[Report Only] Refused to execute inline script ...` — то же.
- `Failed to load resource: 401` — ожидаемо: `/auth/me` до входа.

**Page errors:** нет. **Network failures (requestfailed):** нет.

**API-вызовы на входе:** `401 GET /api/v1/auth/me`, `200 GET /api/v1/auth/oidc/providers`,
`401 POST /api/v1/auth/login` — поведение корректное.

---

## 14. Обзор production-данных — NOT TESTED

Количество организаций/пользователей/контактов/сделок/задач/импортов/событий
аудита **не получено**: агрегатные запросы требуют авторизации. Персональные
данные не выводились.

---

## 15. Дефекты

| ID | Severity | Компонент | Описание | Шаги воспроизведения | Предлагаемое исправление |
| --- | --- | --- | --- | --- | --- |
| **BLOCKER-1** | P0 (для QA) | Учётные данные | Предоставленные email+пароль дают `401`. Кириллица в email («почта-admin@gmail.com») указывает на неверный email. | `POST /api/v1/auth/login {"email":"почта-admin@gmail.com","password":…}` → `401`. | Владельцу: выдать точный email администратора. Не дефект кода. |
| **D-1** | **P1** | Deployment config | `/ready` сообщает `commit=8d350b0`, хотя развёрнут код `main` (бандл совпал с `main`, не с `8d350b0`). Причина — устаревшая env-переменная `FORGECRM_COMMIT` на blitz.cloud. | Сравнить `/ready` с хэшами бандла; `npm run build` на `8d350b0` даёт другие хэши. | Обновить `FORGECRM_COMMIT` до SHA `main` (или включить авто-подстановку). Иначе проверка версии вводит в заблуждение. |
| **D-2** | P2 | Безопасность (CSP) | CSP только в режиме `report-only` — фактически не защищает; даёт шум в консоли. | Загрузить `/login`, открыть консоль. | Включить enforcing-CSP (`Content-Security-Policy`), убрав report-only после наблюдения. |
| **D-3** | P3 | Наблюдаемость | `/metrics` без авторизации отдаёт `503` (не `401`) — непонятно, выключен экспортёр или закрыт. | `GET /metrics` → `503`. | Вернуть `401` при выключенном/закрытом экспортёре либо документировать. |

**Дефектов кода в публичной поверхности не выявлено.**

---

## 16. Непроверенные возможности

- Все авторизованные модули: Dashboard, CRM CRUD, Pipeline/Deals, Tasks, Leads,
  Tickets, Import/Export, Reports, Analytics/Forecast/Risk/Simulation.
- Админ-панель, `/admin/audit`, пользователи, роли, permissions, API-ключи,
  активные сессии, история входов, блокировки, экспорт аудита.
- Интеграции: Telegram, Email, Webhooks, OAuth/SSO, MCP — интерфейсы, статусы,
  журналы, retry, защита секретов.
- API: авторизация, RBAC, пагинация, схемы, OpenAPI.
- MCP: `initialize`, `tools/list`, инструменты, JSON-RPC.
- Cookie-атрибуты, CSRF на мутациях, изоляция организаций.

## 17. Ограничения production-тестирования

- Нет доступа к БД и env приложения — выводы о commit сделаны по артефактам сборки.
- Внешние отправки (email/Telegram/webhooks) не выполнялись.
- Destructive-операции, массовый импорт, изменение настроек — не выполнялись.
- Перебор паролей исключён из-за блокировки после 8 попыток.

## 18. Разделение по состоянию

- **Реально работают:** доступность и здоровье; SPA-fallback; защита маршрутов;
  форма входа и валидация; негативная аутентификация (401/422); rate limiting;
  i18n (RU/EN); темы Light/Dark/System; адаптивность 390/1366/1440; security-заголовки.
- **Частично работают:** API (только неавторизованные ответы); CSP (report-only).
- **UI без подтверждённого backend:** админ-страницы и модули существуют в бандле,
  но их данные не проверялись.
- **Заблокировано внешними сервисами:** Telegram/Email/Webhooks/OIDC (OIDC выключен).
- **Нельзя проверить без разрешения/доступа:** все авторизованные сценарии —
  блокирует неверная учётная запись.

---

## 19. Итоговая оценка

**Статус: LIVE QA BLOCKED.**

Публичная поверхность опубликованного ForgeCRM работает корректно и безопасно:
приложение живо, маршруты защищены, вход и валидация работают, ответы API и
ошибки соответствуют контракту, заголовки безопасности сильные, UI/i18n/темы/
адаптивность — в порядке, развёрнут код `main`. Однако **основная миссия — аудит
продукта под администратором — не выполнена**, потому что предоставленные
учётные данные не проходят аутентификацию. Пока владелец не даст верный email
администратора (или пароль), ни один авторизованный модуль нельзя оценить, и
выдавать по ним PASS было бы неверно.

**Немедленное действие владельца:** подтвердить точный email администратора и
проверить пароль. После этого тот же сценарий можно повторить и закрыть
`NOT TESTED`-разделы.

**Отдельно исправить:** D-1 (устаревший `FORGECRM_COMMIT` — искажает проверку
версии на production).
