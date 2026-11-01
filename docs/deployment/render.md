# Развёртывание ForgeCRM на Render

Это руководство разворачивает работающий ForgeCRM (API + фоновый воркер +
веб-интерфейс) на Render с помощью Blueprint в файле `render.yaml` в корне
репозитория.

## Что где работает

Корневой `Dockerfile` создаёт один образ с тремя вещами:

1. **API** (`forgecrm-api`) — HTTP-сервер и веб-интерфейс, который он обслуживает.
2. **Воркер** (`forgecrm-worker`) — фоновый процесс: задания импорта/экспорта,
   разбор outbox, доставка вебхуков, планировщик workflow и хранение.
3. **Веб-интерфейс** — собранное React-приложение (SPA), обслуживаемое API из
   `/app/public`. Один адрес отвечает и на API, и на UI, в одном источнике,
   поэтому cookie сессии и двойная отправка CSRF работают без CORS.

`render.yaml` объявляет два сервиса, которые собирают тот же образ и запускают
разные точки входа, плюс одну управляемую базу PostgreSQL. Отдельного статического
сайта нет: интерфейс обслуживает API, что держит их в одном источнике.

## Развёртывание

1. Отправьте эту ветку (или смёрджите её) и откройте панель Render.
2. **New → Blueprint**, затем выберите репозиторий. Render прочитает `render.yaml`.
3. Примите предложенный план: `forgecrm-api` (web), `forgecrm-worker` (worker) и
   базу PostgreSQL `forgecrm-db`.
4. Render внедрит `PORT` и `FORGECRM_DATABASE_URL` для базы. `FORGECRM_SECRET_KEY`
   генерируется один раз на веб-сервисе и передаётся воркеру.

## Переменные окружения

Задайте их на сервисе **forgecrm-api**. Значения с пометкой *внедряется*
предоставляет Render, и задавать их вручную нельзя.

| Переменная | Значение | Примечания |
| --- | --- | --- |
| `FORGECRM_ENV` | `production` | Обязательна. |
| `FORGECRM_DATABASE_URL` | *внедряется* | Строка подключения к управляемому PostgreSQL. Приложение читает сначала `FORGECRM_DATABASE_URL` и падает обратно на `DATABASE_URL`. |
| `FORGECRM_SECRET_KEY` | `generateValue: true` | Обязательна. Шифрует сохранённые учётные данные интеграций; её потеря делает их невосстановимыми. |
| `FORGECRM_BASE_URL` | *из `RENDER_EXTERNAL_URL`* | Обязательна в production. Blueprint копирует сюда собственный https-адрес сервиса (`RENDER_EXTERNAL_URL`), поэтому хардкодить имя хоста не нужно. Заданное вручную значение имеет приоритет. |
| `FORGECRM_MIGRATE_ON_START` | `true` | Задаётся на сервисе **forgecrm-api**. По умолчанию флаг выключен; включите его, чтобы API применил миграции перед обслуживанием. |
| `FORGECRM_ROLE` | `worker` | Задаётся на сервисе **forgecrm-worker**. Выбирает бинарник входа образа, чтобы сервис-воркер не поднялся как второй API. |
| `PORT` | *внедряется* | Его задаёт Render; API слушает `0.0.0.0:$PORT`. |
| `FORGECRM_DATABASE_MAX_CONNS` | `6` (api), `4` (worker) | Держите пулы небольшими на тарифе starter. |
| `FORGECRM_RATE_LIMIT_STORE` | `memory` | Одна реплика, поэтому лимитер в процессе корректен. |

Версию и commit API сообщает через `/ready` и строку старта. Render не передаёт
build args, поэтому commit берётся из `RENDER_GIT_COMMIT`, а версия — из
`FORGECRM_VERSION` (если задана). Без этого в логах было бы `version=dev
commit=unknown`.

Воркер ждёт схему, которую применяет API, прежде чем начать забирать работу.

## Создание первого администратора

У API есть одноразовая команда bootstrap. В панели Render откройте **Shell**
сервиса **forgecrm-api** и выполните:

```sh
FORGECRM_BOOTSTRAP_PASSWORD='choose-a-strong-password' \
  forgecrm-api -bootstrap-admin -bootstrap-email you@example.com
```

Пароль передаётся через окружение, поэтому он никогда не попадает в список
процессов. Затем войдите и создайте остальную команду со страниц администрирования.

## Проверка развёртывания

```sh
curl -fsS https://your-app.onrender.com/health   # -> {"status":"ok"}
curl -fsS https://your-app.onrender.com/ready    # -> {"status":"ready"}
curl -fsS -o /dev/null -w '%{http_code}\n' \
  https://your-app.onrender.com/contacts         # -> 200 (клиентский маршрут SPA)
```

## Примечания

- Blueprint задаёт `FORGECRM_LOG_FORMAT=json`, что подходит просмотрщику логов Render.
- Если вы разворачиваете больше одного экземпляра API, задайте
  `FORGECRM_RATE_LIMIT_STORE=redis` и `FORGECRM_REDIS_URL`, иначе лимит действует
  на каждый экземпляр отдельно.
