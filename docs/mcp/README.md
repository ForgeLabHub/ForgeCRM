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
| `GET` | `/api/v1/mcp/credentials` | List the MCP client credentials in the organization. |
| `POST` | `/api/v1/mcp/credentials` | Mint a credential; the plaintext token is returned once. |
| `POST` | `/api/v1/mcp/credentials/{id}/rotate` | Issue a fresh token for an existing credential. |
| `DELETE` | `/api/v1/mcp/credentials/{id}` | Revoke a credential. |

`GET /api/v1/mcp/tools` and `POST /api/v1/mcp/call` are conveniences for ForgeCRM's
own interface, which is not an MCP client. They are documented here only so the
three surfaces are not confused: only `POST /api/v1/mcp` speaks the protocol.

The credential routes are session-only: a credential may not mint another, so an
external client cannot widen its own reach. Reading the list needs
`mcp.credentials.read`; minting, rotating and revoking need `mcp.credentials.manage`.

## Authentication

The protocol endpoint accepts either credential:

- the session cookie the web interface uses, or
- an `Authorization: Bearer fcmcp_…` header carrying an MCP client credential.

A session is tried first, because the admin UI already has one. A request with a
cookie that does not resolve is not retried against the header: presenting an
expired session is an authentication failure, not an invitation to try a different
credential. A request with no valid credential is refused with `401` before any
tool logic runs.

### MCP client credentials

A credential is a personal access token bound to one organization and one owner.
It carries an explicit permission list, and a tool call is authorized against that
list — never the owner's roles. A read-only credential therefore cannot mutate CRM
data even when its owner is an administrator. The scope must be a subset of what
the caller holds, so a credential can narrow authority but never widen it.

Only the SHA-256 of the token is stored, so the plaintext is shown exactly once, at
creation or rotation. The list reports a non-secret prefix (`fcmcp_ab12cd34…`) so a
token can be told apart without being recoverable.

A credential stops working immediately when it is revoked, when its `expires_at`
passes, when its owner is blocked or suspended, or when the owner's membership in
the organization is removed. Create, rotate and revoke are audited; the token value
is never recorded.

A per-credential rate limit (`FORGECRM_MCP_REQUESTS_PER_MIN`, default `120`) bounds
a noisy client. It is keyed by the credential id, so one client cannot spend another
client's budget.

### Managing credentials

An operator with `mcp.credentials.manage` manages them at `/admin/mcp-credentials`:

1. **New credential** — name it, choose an expiry (or none), and tick the
   permissions it may use. The permission picker only offers what the operator
   holds.
2. **Copy the token** — it is shown once and cannot be recovered. Store it in the
   client's secret store.
3. **Rotate** issues a fresh token with the same scope; the previous token stops
   working at once.
4. **Revoke** disconnects the client immediately. The row is kept for the audit
   trail and is swept by retention once it can no longer authenticate.

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

Every message goes to `POST /api/v1/mcp` with `Content-Type: application/json` and
one of the two credentials. These examples use `$BASE` for the origin and
`$TOKEN` for an MCP credential (a session cookie works identically if you prefer).

**1. Initialize**

```bash
curl -s "$BASE/api/v1/mcp" -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
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
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}'
# 202
```

**3. List the tools**

```bash
curl -s "$BASE/api/v1/mcp" -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
```

**4. Call one**

```bash
curl -s "$BASE/api/v1/mcp" -H "Content-Type: application/json" \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call",
       "params":{"name":"search_accounts","arguments":{"query":"Acme"}}}'
```

The `content[0].text` member holds the result as a JSON string, which is what the
protocol specifies for a text result.

## Client configuration

An MCP client needs a URL and a credential.

- **Transport:** streamable HTTP, `POST /api/v1/mcp`.
- **URL:** `https://<your-host>/api/v1/mcp`.
- **Credential:** `Authorization: Bearer fcmcp_…`, minted at
  `/admin/mcp-credentials`.

The token never expires unless an expiry was set, and it can be revoked on its own
without touching the owner's login. Scope it to the tools the client needs: the
least-privilege list is what limits the damage if the token leaks.

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
