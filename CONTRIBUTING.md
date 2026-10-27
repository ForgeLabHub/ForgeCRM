# Contributing to ForgeCRM

## Before you start

Open an issue describing the change before writing significant code. For a bug, include
the exact request, the observed response and the expected one.

## Development environment

Requires Go 1.24 and Docker with the Compose plugin.

```bash
git clone https://github.com/geff1778/ForgeCRM.git
cd ForgeCRM/backend
go build ./...
go test ./...
```

For a full stack, follow the Quick start in [README.md](README.md).

## What a change must include

Every functional change must come with:

1. **Tests.** A new endpoint needs a test for the success path, the authorisation
   failure and the validation failure. A bug fix needs a test that fails before the fix.
2. **A migration**, if the schema changes. Add `NNNN_name.up.sql` and
   `NNNN_name.down.sql` under `backend/internal/migrations`. Never edit an applied
   migration; add a new one.
3. **Permission wiring**, if the endpoint exposes data. Add the permission to the
   catalog and to the roles that should hold it, and check it on the server. Hiding a
   button is not access control.
4. **Documentation**, if behaviour, configuration or an API contract changes.

## Standards

- Go code must be `gofmt`-clean and pass `go vet`.
- Follow the existing style. Prefer small, explicit functions over clever ones.
- Write comments that explain *why*, not *what*. Do not narrate the change history in
  the code; that belongs in the commit message.
- Do not add a dependency for something the standard library does well.
- Never commit secrets, real customer data or a populated `.env`.

## Database changes

- Use explicit column lists; never `SELECT *` in application code.
- Every query that filters by organization must include `organization_id`.
- Wrap multi-statement writes in a transaction and record an audit entry.
- Add indexes for the columns a query filters or sorts on.

## Security expectations

ForgeCRM handles corporate data. Changes touching authentication, authorisation,
secrets, file handling or outbound HTTP receive extra scrutiny.

- Enforce permissions on the server for every request.
- Treat uploaded files and archive contents as hostile input. See
  `backend/internal/importx/safe`.
- Do not weaken the SSRF guard on outbound requests.
- Do not log credentials, tokens or full personal data.

Report a vulnerability privately as described in [SECURITY.md](SECURITY.md).

## Commits and pull requests

- Write a commit subject in the imperative mood, under 72 characters.
- One logical change per pull request.
- Describe what changed and how you verified it. Include the commands you ran and their
  results. A claim of "works" without evidence will be sent back.
- Update the pull request description if the scope changes during review.

## License

No license has been chosen yet. By contributing you agree that your contribution may be
distributed under whatever license the project owner selects. Ask before contributing if
this matters to you.
