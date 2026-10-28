# MCP — connecting an AI client

ForgeCRM exposes its CRM tools over the Model Context Protocol, so a standard MCP
client can search accounts, read a customer summary, list tasks, read the pipeline
and — with an explicit acknowledgement — create a task or a ticket, change a deal
stage and pull a report.

The tools are not a separate implementation. Each one calls the same `crm.Service`
the REST API calls and passes through the same permission check, so an MCP client
can never do more than the signed-in user could do through the web interface.

## Endpoints

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/v1/mcp` | The MCP protocol (JSON-RPC 2.0). This is what an MCP client uses. |
| `GET` | `/api/v1/mcp/tools` | The tool catalogue for the admin UI, with the permission each tool needs. |
| `POST` | `/api/v1/mcp/call` | A single tool call in the admin UI's own shape. |

`GET /api/v1/mcp/tools` and `POST /api/v1/mcp/call` are conveniences for ForgeCRM's
own interface, which is not an MCP client. They are documented here only so the
three surfaces are not confused: only `POST /api/v1/mcp` speaks the protocol.

## Authentication

The protocol endpoint authenticates with the same session cookie the web
interface uses. A request without a valid session is refused with `401` before any
tool logic runs.

> **Limitation.** There is no dedicated MCP credential yet. An MCP client therefore
> needs a browser session cookie, which is awkward for a long-running client and
> cannot be revoked independently of the user's login. A scoped, revocable MCP key
> with its own read/write split is tracked as Phase 3, item 3.3 in
> [`docs/ROADMAP.md`](../ROADMAP.md). Do not build a production integration on the
> assumption that a separate key exists.

## The protocol

JSON-RPC 2.0. `id` is echoed back unchanged and may be a string or a number.

### Methods

| Method | Kind | Result |
|---|---|---|
| `initialize` | request | `protocolVersion`, `capabilities`, `serverInfo` |
| `notifications/initialized` | notification | none (the server answers `202`) |
| `notifications/cancelled` | notification | none |
| `ping` | request | an empty object |
| `tools/list` | request | `{ "tools": [ … ] }` |
| `tools/call` | request | `{ "content": [ { "type": "text", "text": "…" } ], "is_error": false }` |

A message without an `id` is a notification and receives an empty `202` with no
body. Replying to a notification is a protocol error, so the server does not.

### Protocol version

The server supports `2025-06-18` and `2024-11-05`. It echoes the version the client
asks for when it supports it, and otherwise reports its own. An unsupported version
is not an error: a client whose messages are otherwise understood should not be
locked out over a version string.

### Capabilities

The `initialize` result advertises `tools` and nothing else. `resources` and
`prompts` are not implemented, so they are not advertised — a client that read
either in the capabilities would call a method this server answers with
`method not found`.

### Error codes

| Code | When |
|---|---|
| `-32700` | the body is not valid JSON |
| `-32600` | `jsonrpc` is not `"2.0"`, or `method` is missing |
| `-32601` | the method is unknown, or the tool does not exist |
| `-32602` | the arguments are invalid, the tool requires confirmation and did not receive it, or the caller lacks the tool's permission |
| `-32603` | an internal failure |

A failure carries the HTTP status in `error.data.status`, so a client can tell a
permission refusal (`403`) from a bad argument (`400`).

## The tools

| Tool | Permission | Changes data |
|---|---|---|
| `search_accounts` | `accounts.read` | no |
| `account_summary` | `accounts.read` | no |
| `list_tasks` | `tasks.read` | no |
| `pipeline_overview` | `pipelines.read` | no |
| `get_report` | `reports.read` | no |
| `create_task` | `tasks.create` | **yes** |
| `create_ticket` | `tickets.create` | **yes** |
| `update_deal_stage` | `deals.change_stage` | **yes** |

There is no tool for raw SQL, shell, filesystem access or an unbounded export, by
construction: the registry contains only CRM operations.

### Confirming a write

A tool that changes data is refused unless the call carries `confirm: true` in its
arguments:

```json
{"jsonrpc":"2.0","id":3,"method":"tools/call",
 "params":{"name":"create_task","arguments":{"title":"Call Acme","confirm":true}}}
```

The requirement is published in the tool's `inputSchema`, so a client can see it
before calling rather than receiving a refusal it cannot explain. The
acknowledgement travels in the arguments, not in a transport-level flag, so it is
part of the same message the permission check inspects.

## A worked session

Every message goes to `POST /api/v1/mcp` with the session cookie and
`Content-Type: application/json`. These examples use `$BASE` for the origin and
`$COOKIE` for the session cookie.

**1. Initialize**

```bash
curl -s "$BASE/api/v1/mcp" -H "Content-Type: application/json" \
  -H "Cookie: forgecrm_session=$COOKIE" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}'
```

```json
{"jsonrpc":"2.0","id":1,"result":{
  "protocolVersion":"2025-06-18",
  "capabilities":{"tools":{}},
  "serverInfo":{"name":"forgecrm","version":"…"}}}
```

**2. Acknowledge the handshake** (a notification — no `id`, so no reply body)

```bash
curl -s -o /dev/null -w '%{http_code}\n' "$BASE/api/v1/mcp" -H "Content-Type: application/json" \
  -H "Cookie: forgecrm_session=$COOKIE" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
# 202
```

**3. List the tools**

```bash
curl -s "$BASE/api/v1/mcp" -H "Content-Type: application/json" \
  -H "Cookie: forgecrm_session=$COOKIE" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

**4. Call one**

```bash
curl -s "$BASE/api/v1/mcp" -H "Content-Type: application/json" \
  -H "Cookie: forgecrm_session=$COOKIE" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"search_accounts","arguments":{"query":"Acme"}}}'
```

The `content[0].text` member holds the result as a JSON string, which is what the
protocol specifies for a text result.

## Client configuration

An MCP client needs a URL and a credential. ForgeCRM's credential is the session
cookie, so a client must be configured to send it.

- **Transport:** streamable HTTP, `POST /api/v1/mcp`.
- **URL:** `https://<your-host>/api/v1/mcp`.
- **Credential:** the `forgecrm_session` cookie.

Because the credential is a browser session, the practical options today are a
client that can attach a cookie header, or a small proxy that holds the cookie and
forwards the protocol messages. The scoped-key work in item 3.3 is what removes
this constraint; until then, treat the cookie as a short-lived credential and
expect to re-authenticate when the session expires.

## What MCP does not change

- **Authorization.** A tool call runs the caller's own permission check. A client
  cannot reach a tool the user could not reach in the interface, and the check is
  the same code path, so the two cannot drift apart.
- **Tenant scope.** Tools read and write inside the caller's organization. An
  account in another organization is not returned, and its identifier cannot be
  guessed into a result.
- **Audit.** Every tool invocation is recorded, including a denied attempt, with
  the tool name and whether it was allowed.
- **Prompt injection.** Instructions found inside imported rows, documents or
  external responses do not change what a tool is allowed to do. The permission
  check reads the caller's roles, not the content being processed.

## See also

- [`docs/api/README.md`](../api/README.md) — the REST API, including the generated
  OpenAPI document.
- [`docs/ARCHITECTURE.md`](../ARCHITECTURE.md) §8 — where the AI layer sits in the
  service graph.
- [`docs/ROADMAP.md`](../ROADMAP.md) — Phase 3, the MCP work that remains.
