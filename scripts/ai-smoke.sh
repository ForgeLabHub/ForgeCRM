#!/usr/bin/env bash
#
# Проверка, что настроенный AI-провайдер отвечает. По умолчанию — OpenRouter с
# моделью google/gemma-2-9b-it:free.
#
#   ./scripts/ai-smoke.sh              отправить «Привет» и напечатать ответ
#   ./scripts/ai-smoke.sh "Как дела?"  отправить свой текст
#
# Скрипт читает те же переменные, что и приложение, поэтому проверяет именно ту
# конфигурацию, которую увидит сервер:
#   FORGECRM_AI_BASE_URL  или LLM_BASE_URL   (по умолчанию https://openrouter.ai/api/v1)
#   FORGECRM_AI_MODEL     или LLM_MODEL      (по умолчанию google/gemma-2-9b-it:free)
#   FORGECRM_AI_API_KEY   или OPENROUTER_API_KEY
#   FORGECRM_AI_HTTP_REFERER, FORGECRM_AI_TITLE — необязательные заголовки
#
# Ключ никогда не печатается. Скрипт делает ровно один запрос и ничего не пишет
# в CRM: это проверка соединения, а не изменение данных.

set -uo pipefail

prompt="${1:-Привет}"

base="${FORGECRM_AI_BASE_URL:-${LLM_BASE_URL:-https://openrouter.ai/api/v1}}"
model="${FORGECRM_AI_MODEL:-${LLM_MODEL:-google/gemma-2-9b-it:free}}"
key="${FORGECRM_AI_API_KEY:-${OPENROUTER_API_KEY:-}}"

# Тот же нормализатор, что и в config.go: если задан только хост, добавляем
# версионный путь /api/v1, иначе запрос уйдёт на /chat/completions и вернёт 404.
case "${base%/}" in
  https://openrouter.ai|http://openrouter.ai) base="https://openrouter.ai/api/v1" ;;
esac

if [[ -z "${key}" ]]; then
  printf '  \033[31mfail\033[0m  не задан ключ: установите OPENROUTER_API_KEY (или FORGECRM_AI_API_KEY)\n' >&2
  exit 1
fi

endpoint="${base%/}/chat/completions"

# Тело запроса собираем через jq, если он есть: это защищает от кавычек и
# переводов строк в подсказке. Без jq — простой printf.
if command -v jq >/dev/null 2>&1; then
  body="$(jq -n --arg m "${model}" --arg p "${prompt}" \
    '{model:$m, messages:[{role:"user", content:$p}], max_tokens:256}')"
else
  body="$(printf '{"model":"%s","messages":[{"role":"user","content":"%s"}],"max_tokens":256}' \
    "${model}" "${prompt}")"
fi

# Дополнительные заголовки атрибуции добавляем только когда они заданы, чтобы
# пустой HTTP-Referer не попал в аналитику OpenRouter.
hdr=()
[[ -n "${FORGECRM_AI_HTTP_REFERER:-}" ]] && hdr+=(-H "HTTP-Referer: ${FORGECRM_AI_HTTP_REFERER}")
[[ -n "${FORGECRM_AI_TITLE:-}" ]] && hdr+=(-H "X-Title: ${FORGECRM_AI_TITLE}")

printf 'ForgeCRM AI smoke test\n'
printf '  endpoint: %s\n' "${endpoint}"
printf '  model:    %s\n' "${model}"
printf '  prompt:   %s\n\n' "${prompt}"

resp="$(curl -sS -w $'\n%{http_code}' -X POST "${endpoint}" \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer ${key}" \
  "${hdr[@]}" \
  -d "${body}" 2>&1)" || { printf '  \033[31mfail\033[0m  запрос не выполнен\n' >&2; exit 1; }

status="$(printf '%s' "${resp}" | tail -n1)"
payload="$(printf '%s' "${resp}" | sed '$d')"

if [[ "${status}" != "200" ]]; then
  printf '  \033[31mfail\033[0m  провайдер вернул HTTP %s\n' "${status}" >&2
  printf '%s\n' "${payload}" >&2
  exit 1
fi

if command -v jq >/dev/null 2>&1; then
  printf '  \033[32mok\033[0m    ответ модели:\n\n'
  printf '%s' "${payload}" | jq -r '.choices[0].message.content // "(пустой ответ)"'
else
  printf '  \033[32mok\033[0m    ответ модели (raw):\n\n%s\n' "${payload}"
fi
