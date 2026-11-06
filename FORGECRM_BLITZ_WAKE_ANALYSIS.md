# ForgeCRM on Blitz Cloud — sleep/wake semantics

Read-only investigation. No deployment, no production-config change, no attempt to
circumvent the free-tier policy. No application code or config was modified.

- App: `https://forgecrm.forgecrm.blitz.cloud` (308 → `https://forgecrm.forgecrm.prod.page`)
- Blitz MCP endpoint: `https://mcp.blitz.cloud/forgecrm`
- Evidence gathered: 2026-11-06, account plan `free`.

> Redaction: `BLITZ_API_KEY`, `RENDER_API_KEY` and `GITHUB_TOKEN` are never printed
> here. The MCP session id is shown only as a placeholder. No secret value appears
> in this document.

---

## 1. What the repository already documents

`docs/deployment/blitz-cloud.md` (section "Поведение на бесплатном тарифе" and the
subsection "Сон и пробуждение: программа приложение не будит") plus the CHANGELOG
entry from commit `a437137` already state, and this investigation confirms:

- The **api** part sleeps after **15 minutes without visitors** and "scales to
  zero" — it holds no memory while asleep.
- **A human browser visit wakes it** (~10 s cold start; a waiting page shows after
  5 s). An automated/headless browser or a spoofed `User-Agent` does **not** count
  as a visitor.
- **Program requests do not wake it.** Uptime monitors, cron, scripts, webhooks and
  API clients get `503` with `x-blitz-gate: asleep` and the app stays asleep.
- The only way for a program to reach the app is the paid `keepAwake` (Pro).
- The **worker** part does not sleep on inactivity; on the free plan it is stopped
  when nothing happens in the account (**24 h** for accounts opened from
  2026-11-05, 2 days for older). It is resumed by **Start** in the dashboard, not by
  a browser visit.
- `.blitz.cloud` answers `308` to `.prod.page` (`x-blitz-gate: redirect`), so
  deployment-check `curl` calls need `-L` and the `.prod.page` address.
- The managed **database sleeps with the app** (it sleeps when every linked app is
  asleep/paused, and starts before the app on the next wake). After a full day
  asleep the account disk is put away, adding a minute or two to the first wake.

`scripts/blitz-verify.sh` reproduces the deployment locally (clean PostgreSQL 17,
migrations twice, bootstrap admin twice, TLS proxy, liveness vs readiness). It does
**not** exercise platform sleep — that is a Blitz gate, outside the image.

`docs/verification/matrix.md` maps the claim to `make blitz-verify` → CI job
`blitz-cloud`. CI at `97e242e` is green (16/16 check runs, including
`Blitz Cloud deployment (root image, clean database)`).

---

## 2. MCP endpoint investigation

### Transport

- `POST https://mcp.blitz.cloud/forgecrm` speaks **Streamable HTTP / JSON-RPC 2.0**
  and returns `mcp-session-id` on `initialize`. `GET` with `Accept:
  text/event-stream` returns **405** — no standalone SSE stream; the session is
  driven by `POST`.
- `Accept: application/json, text/event-stream` is required.

### Auth scheme

- Unauthenticated `initialize` → **401** with:

  ```http
  www-authenticate: Bearer realm="blitz.cloud",
    resource_metadata="https://mcp.blitz.cloud/.well-known/oauth-protected-resource",
    scope="mcp:tools"
  ```

- `GET /.well-known/oauth-protected-resource`:

  ```json
  {"resource":"https://mcp.blitz.cloud/",
   "authorization_servers":["https://login.blitz.cloud/realms/blitz"],
   "scopes_supported":["mcp:tools"],
   "bearer_methods_supported":["header"],
   "resource_name":"blitz.cloud",
   "resource_documentation":"https://blitz.cloud/docs/ai-assistants/"}
  ```

- Sending `Authorization: Bearer <BLITZ_API_KEY>` returns **200** on `initialize`:

  ```json
  {"jsonrpc":"2.0","id":1,"result":{"capabilities":{"logging":{},
   "tools":{"listChanged":true}},"serverInfo":{"name":"blitz.cloud",
   "version":"1.0.0-SNAPSHOT","title":"blitz.cloud",
   "websiteUrl":"https://blitz.cloud"},"protocolVersion":"2024-11-05",
   "resultType":"complete"}}
  ```

  So the API key is accepted as an OAuth bearer token directly (no separate
  credential exchange was needed). No brute force was attempted.

### Tools (47)

`apps_delete, apps_deploy_github, apps_deploy_image, apps_deploy_store, apps_get,
apps_list, apps_pause, apps_restart, apps_resume, apps_run, apps_update,
builds_list, builds_start, dbs_connect, dbs_create, dbs_delete, dbs_list,
dbs_restart, dbs_rotate_password, designer_forward, designer_link, designer_read,
domains_dns_add, domains_dns_delete, domains_list, domains_own_add,
domains_own_check, domains_own_connect, domains_own_list, domains_own_remove,
env_delete, env_get, env_list, env_set, files_download, files_upload_finish,
files_upload_start, logs_tail, network_list, network_set_private, platform_status,
projects_create, projects_list, projects_members, store_docker_search,
store_search, usage_get`

**There is no `wake` tool.** The relevant descriptions:

| Tool | Description (verbatim, key clause) |
| --- | --- |
| `apps_resume` | "Starts a **paused or stopped** app again." — not a sleeping one. |
| `apps_pause` | "Stops an app until it is resumed." |
| `apps_update` | field `keepAwake`: "**Never put to sleep for lack of visitors (Pro)**". |
| `files_download` | "Works whether the app is **awake or asleep**." (does not wake it) |
| `files_upload_start` | "Works whether the app is **awake or asleep**." (does not wake it) |
| `apps_run` | Runs a command "inside the app's **running** container" — needs it awake. |

### Live state (`apps_get forgecrm`, `usage_get`)

```json
// apps_get (excerpt)
{"address":"forgecrm.forgecrm.prod.page","asleep":false,"desiredState":"running",
 "commitSha":"97e242e1378aceeb312c5001b2e65f4cbbcd64bb","keepAwake":false,
 "keepAwakeAllowed":false,"pro":false,"lastLiveAt":"2026-11-06T11:12:48Z",
 "lastTrafficAt":"2026-11-06T14:41:10Z",
 "processes":[{"name":"api","kind":"web","status":"online","asleep":false},
              {"name":"worker","kind":"worker","status":"online","asleep":false}]}

// usage_get (excerpt)
{"plan":"free","sleepsAfterMinutes":15,"workerIdleMinutes":2880,
 "apps":{"used":2,"limit":5},"databases":{"used":1,"limit":1}}
```

Key live facts:

- `plan: "free"`, `sleepsAfterMinutes: 15` → the 15-minute sleep window is a
  platform account setting, not a guess.
- `workerIdleMinutes: 2880` = 48 h (2 days) for this older account.
- `keepAwake: false`, `keepAwakeAllowed: false`, `pro: false` → keepAwake is not
  merely off, it is **not allowed** on this plan; it requires Pro.
- `apps_get` has **no wake field/action**; the only state-changing verbs are
  `apps_resume` (paused/stopped) and `apps_restart`.

### HTTP probe of the app

- `GET https://forgecrm.forgecrm.blitz.cloud/health` → **308** `x-blitz-gate:
  redirect`, `location: .../prod.page/health`.
- `GET https://forgecrm.forgecrm.prod.page/health` → **200**
  `{"service":"forgecrm","status":"ok","version":"main"}`.
- `GET .../prod.page/ready` → **200** `{"commit":"8d350b0c...","status":"ready",
  "version":"main"}`.

The app was awake at probe time (a browser-visited state), so the live `503
asleep` gate could not be re-observed in this session. Its shape is recorded in the
repo doc and was captured in the 2026-11-06 audit: `503`, `retry-after: 900`,
`x-blitz-gate: asleep`, body
`{"status":"asleep","message":"This app is asleep. A program cannot wake it.
Opening it in a browser does. Pro keeps an app awake for a program too.",
"upgradeUrl":"https://dashboard.blitz.cloud/settings?from=gate-wake#usage"}`.

---

## 3. Verdict among (a)–(d)

The four candidate behaviours were:

- (a) sleeps on inactivity; an HTTP request wakes it (maybe cold start);
- (b) sleeps; a single request times out but a retry succeeds;
- (c) the platform stops it and only a Blitz-controlled mechanism can start it (a
  program cannot);
- (d) free tier caps runtime regardless of activity.

**Verdict: (c)** — the platform scales the web part to zero and **a program cannot
start it**; only a human browser visit (or paid `keepAwake`, which keeps it from
sleeping at all) reaches the app.

Evidence:

1. The gate body states it in words: *"A program cannot wake it. Opening it in a
   browser does."* — behaviour (c), explicitly.
2. `x-blitz-gate: asleep` comes from Blitz's gate, **before** the container; the
   app's own process is not running while asleep, so no application code can
   self-wake.
3. `retry-after: 900` = the 15-minute window (`sleepsAfterMinutes: 15`); a retry
   after the interval still hits the gate, so this is not (b)'s transient cold
   start.
4. `apps_get` exposes no wake action; `apps_resume` is scoped to **paused/stopped**,
   not asleep, and `keepAwake` is `keepAwakeAllowed: false` on this plan.
5. Not (d): `usage_get` shows no runtime cap — the app is `desiredState: running`,
   memory-limited (512 MB) but not wall-clock-capped; sleep is triggered by lack of
   **visitors**, not by elapsed runtime.

---

## 4. Can a program wake the app?

**No — not on the free plan.**

- There is no wake tool in the Blitz MCP API. `apps_resume` does not apply to a
  sleeping app (it starts a *paused or stopped* one).
- `files_download` / `files_upload_start` work against a sleeping app but explicitly
  do not wake it.
- An HTTP request (monitor, cron, script, webhook, API client) receives `503
  x-blitz-gate: asleep` and the app stays asleep.
- The only supported way for a program to reach the app is to prevent sleep with
  **`keepAwake`**, and that is **Pro-only** (`keepAwakeAllowed: false`, `pro:
  false`, 2 EUR/mo per app or 7 EUR/mo per account).

A human opening the site in a real browser is the only free wake path.

---

## 5. Evaluation of the three proposed options

### 5.1 External uptime monitor (UptimeRobot / Better Stack / cron-job.org) hitting `/health` or `/ready`

- **Prevents/undoes sleep? No.** Requests from a monitor are program requests; they
  get `503 asleep` and do not wake the app. Blitz's own docs say a check never keeps
  an app awake.
- **Risk:** none to terms per se — it is a normal HTTP client — but it produces
  **false "down" alerts** (the app is "asleep", which Blitz does not consider down)
  and can look like an anti-abuse probe if hammered. Keep it low-frequency at most.
- **Verdict:** does not solve the problem. Only useful as a signal, and even then it
  reports the gate, not the app.

### 5.2 Self-hosted "wake-up worker" outside Blitz with exponential backoff

- **Prevents/undoes sleep? No.** However clever the backoff, the request arrives at
  the Blitz gate, which answers `503` without starting the container. Retrying does
  not cross into the container.
- **Risk:** if it polls aggressively it resembles the "spam the endpoint" pattern
  the platform's gate exists to throttle; backoff is essential. It still cannot
  wake anything.
- **Verdict:** does not solve the problem. It is just an external monitor with more
  code.

### 5.3 Scheduled GitHub Actions healthcheck

- **Prevents/undoes sleep? No**, for the same reason: it is a program request.
  Additionally the default `GITHUB_TOKEN` is scoped to the repository and cannot be
  used to call Blitz; the Blitz API key would have to be a repo secret, and even
  then there is no wake call to make.
- **Risk:** storing the Blitz API key as a repo secret on a public repo widens the
  blast radius of a leak; a scheduled job also produces noisy failures every time
  the app is asleep.
- **Verdict:** does not solve the problem; adds secret-handling risk.

**Common conclusion:** all three are *monitors*, not *wakers*. Under behaviour (c)
none of them can start the app, because the gate — not the app — decides.

---

## 6. Recommendation for ForgeCRM

Accept the platform's semantics; do not fight them.

1. **Keep Blitz free tier and accept cold starts (recommended).** Document that the
   web part sleeps after 15 minutes without visitors and that a human browser visit
   wakes it in ~10 s. The worker keeps processing (it does not sleep on inactivity),
   so background work continues. Add a small note in the app or ops doc: "first
   visit after idle may take ~10 s".
2. **Do not deploy an uptime monitor or wake-worker against the free tier.** It
   cannot wake the app and only generates false downtime signals (and, at high
   frequency, looks like the abuse the gate is designed to stop).
3. **If programmatic reachability is required, upgrade Blitz** — set `keepAwake:
   true` via `apps_update` (needs `keepAwakeAllowed`, i.e. Pro). This is the only
   supported way for a program to reach the app.
4. **Alternatively move to Render** (a `render.yaml` exists) or another host whose
   free tier does not scale web to zero, if the product needs always-on
   programmatic access without paying Blitz. Render's starter plan keeps the web
   service running; the free web tier also idles, so choose the plan accordingly.

No implementation was performed; this document is analysis only.

---

## Appendix — raw probe transcript (secret-free)

```
POST /forgecrm  (no auth)
  -> 401  www-authenticate: Bearer realm="blitz.cloud",
          resource_metadata=".../.well-known/oauth-protected-resource",
          scope="mcp:tools"

GET /.well-known/oauth-protected-resource
  -> 200  {"resource":"https://mcp.blitz.cloud/",
           "authorization_servers":["https://login.blitz.cloud/realms/blitz"],
           "scopes_supported":["mcp:tools"],"bearer_methods_supported":["header"],
           "resource_name":"blitz.cloud"}

POST /forgecrm  (Authorization: Bearer <BLITZ_API_KEY>)
  -> 200  mcp-session-id: <redacted>
          {"result":{"capabilities":{...},"serverInfo":{"name":"blitz.cloud",...},
           "protocolVersion":"2024-11-05"}}

POST /forgecrm  tools/list  -> 47 tools (names in section 2)

GET /forgecrm  Accept: text/event-stream  -> 405 (no standalone SSE stream)

GET https://forgecrm.forgecrm.blitz.cloud/health
  -> 308  x-blitz-gate: redirect  location: .../prod.page/health
GET https://forgecrm.forgecrm.prod.page/health
  -> 200  {"service":"forgecrm","status":"ok","version":"main"}
GET https://forgecrm.forgecrm.prod.page/ready
  -> 200  {"commit":"8d350b0c...","status":"ready","version":"main"}
```
