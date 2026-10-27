# Security Policy

## Reporting a vulnerability

Report suspected vulnerabilities privately. Do not open a public issue.

Use GitHub's private vulnerability reporting on the repository
(Security -> Report a vulnerability), or contact the maintainers directly.

Include:

- what the issue is and where it is,
- how to reproduce it,
- the impact you believe it has,
- any suggested fix.

You will receive an acknowledgement. Please allow time for a fix before disclosing
publicly.

## Supported versions

The project is pre-1.0 and under active development. Only the current `main` branch is
supported. There are no backports.

## Security model

**Authentication.** Passwords are hashed with Argon2id. Sessions use a server-side
record referenced by an opaque cookie, marked `HttpOnly` and `SameSite`, and `Secure`
in production. Mutating requests require a CSRF token. TOTP second factors are
supported. Repeated failed logins are rate limited and lock the account.

**Authorisation.** Every request is checked on the server against a permission catalog.
Roles are named permission sets, not hard-coded checks. Data access is scoped to the
caller's organization. Hiding a control in a user interface is not a security boundary.

**Tenant isolation.** Every query that reads or writes tenant data filters by
`organization_id`. A record identifier from another organization returns 404, not 403,
so the API does not confirm that the record exists.

**Secrets.** Integration credentials are encrypted at rest with the key from
`FORGECRM_SECRET_KEY`. That key is never logged or returned by an API. Losing it makes
stored integration credentials unrecoverable.

**Untrusted input.** Uploaded archives are hostile input. Extraction enforces entry
count, total size, nesting depth and compression-ratio limits, rejects absolute and
escaping paths, refuses symlinks, and runs in a private directory that is never served
over HTTP. See `backend/internal/importx/safe`.

**Outbound requests.** Integration endpoints are checked before connecting. Loopback,
link-local, private and cloud metadata addresses are refused to prevent SSRF.

## Known limitations

These are real and should be considered when deploying:

- **No license file.** The distribution model is undecided.
- **The administration panel does not exist yet.** The API is the interface.
- **Rate limiting is in-process.** With multiple API replicas, limits apply per
  replica rather than globally.
- **No external security audit has been performed.** Automated analysis and targeted
  tests have run; a professional review has not.
- **OIDC is implemented but untested against a real identity provider** in this
  environment.

## Hardening checklist for production

- [ ] Set `FORGECRM_ENV=production`; development mode returns reset tokens in responses.
- [ ] Set `FORGECRM_COOKIE_SECURE=true` and serve over HTTPS.
- [ ] Set `FORGECRM_BASE_URL` to the public HTTPS URL.
- [ ] Set a unique, strong `FORGECRM_SECRET_KEY` and store it in a secret manager.
- [ ] Set `FORGECRM_METRICS_TOKEN` before exposing `/metrics`.
- [ ] Put the database on a private network and do not publish its port.
- [ ] Terminate TLS at the reverse proxy; do not expose the API port directly.
- [ ] Take and test a backup before serving real data.
