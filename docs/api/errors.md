# Error responses

Every failed request returns the same JSON shape, whatever the endpoint. It
carries [RFC 9457](https://www.rfc-editor.org/rfc/rfc9457) Problem Details
members at the top level, so a generic client, gateway or proxy can act on a
failure without knowing this API, and repeats the machine-readable code under
`error` for callers that predate that.

```json
{
  "type": "urn:forgecrm:error:validation_failed",
  "title": "Validation failed",
  "status": 422,
  "detail": "validation failed",
  "instance": "/api/v1/contacts",
  "retryable": false,
  "request_id": "6f1c9d3a-...",
  "correlation_id": "6f1c9d3a-...",
  "error": {
    "code": "validation_failed",
    "message": "validation failed",
    "details": { "email": "is required" }
  }
}
```

The top-level members and the `error` object are two views of one value, so they
cannot disagree. `detail` always equals `error.message`, and `type` always ends
with `error.code`.

## Members

| Member | Meaning |
|---|---|
| `type` | Problem class, as `urn:forgecrm:error:<code>`. A URN rather than an https URL on purpose: no dereferenceable error catalogue is operated, and a link that 404s is worse than an identifier that never claimed to resolve. Branch on `error.code`, not on this. |
| `title` | Short summary of the problem class, stable per code. |
| `status` | The HTTP status, repeated as a number. |
| `detail` | What went wrong, safe to show a user. Never contains SQL, file paths, hostnames or a stack trace. |
| `instance` | The request path. The query string is removed, because it can carry tokens and personal data that must not be echoed into logs or client-side error reporting. |
| `retryable` | Whether resending the identical request could succeed. See below. |
| `request_id` | Correlation identifier, also returned in the `X-Request-Id` response header. |
| `correlation_id` | The same value as `request_id`. Both names are emitted because the log pipeline and the frontend read different ones. |
| `error.code` | The stable identifier to branch on. |
| `error.message` | The message. Equal to `detail`. |
| `error.details` | Field-level messages, present on validation failures. |

## `retryable`

`retryable` answers one question: could resending this exact request succeed?

It is not derivable from the status code, which is why it is reported
explicitly. A `500` may be a transient fault or a deterministic defect, and
retrying a defect is how a small incident becomes a large one, so `500` reports
`false`. Only `429`, `502`, `503` and `504` report `true`:

| Status | `retryable` | Reasoning |
|---|---|---|
| `429 Too Many Requests` | `true` | "Not now" — wait, then retry. |
| `502 Bad Gateway` | `true` | An upstream hop failed; a different hop may succeed. |
| `503 Service Unavailable` | `true` | A subsystem is disabled or restarting. |
| `504 Gateway Timeout` | `true` | The upstream may have completed; retry with an idempotency key. |
| `500 Internal Server Error` | `false` | Usually a defect. Retrying re-runs the defect. |
| `4xx` other than 429 | `false` | The request must change. |

A client that retries should still bound its attempts and use exponential
backoff with jitter. Retrying a non-idempotent write without an idempotency key
can duplicate the effect even where `retryable` is `true`.

## Codes

| Code | Typical status | Meaning |
|---|---|---|
| `bad_request` | 400 | The request is malformed. |
| `validation_failed` | 422 | A field failed validation; see `error.details`. |
| `unauthorized` | 401 | No session, or it expired or was revoked. Sign in again. |
| `forbidden` | 403 | Authenticated but not permitted. Retrying will not help. |
| `not_found` | 404 | No such resource, or none visible to this organization. |
| `conflict` | 409 | The request conflicts with existing state, such as a duplicate. |
| `rate_limited` | 429 | Too many requests. |
| `payload_too_large` | 413 | The request body exceeds the endpoint's limit. |
| `unsupported` | 400 | The operation is not supported for this resource. |
| `internal_error` | 500 | An unexpected failure. The detail is deliberately generic. |
| `disabled` | 503 | The subsystem is switched off by configuration. |
| `mfa_required` | 401 | A second factor must be completed. |

## What is never returned

Error responses do not contain stack traces, SQL, database or table names,
internal IP addresses, filesystem paths, secret values, tokens, or the request
body. An unrecognised error is reported as `internal_error` with a generic
detail; the real cause is written to the server log against the same
`request_id`, which is what to quote when reporting a problem.

## Frontend behaviour

`ApiError` in `frontend/src/api/client.ts` exposes `status`, `code`, `details`,
`requestId` and `retryable`. It prefers the nested `error` object, which carries
field-level details, and falls back to the top-level `detail` so that a service
returning bare Problem Details still produces a readable message rather than a
generic one. `errorMessage()` and `fieldErrors()` are the helpers components use
to turn a thrown value into text.
