# ADR 0008 — MySQL/MariaDB as a datastore: the verified blockers

- **Status:** Accepted (direction deferred; blockers recorded)
- **Date:** 2026-10-29
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0001 (hybrid architecture)

## Context

The roadmap has carried "MySQL/MariaDB adapter" as a separate direction. The
question that has to be answered before any code is written is not "can the
`database` package be made dialect-aware" — it is "what does the application
actually depend on PostgreSQL for, and how much of that is the storage engine
rather than the queries?"

This ADR answers that from the code, not from intent. The counts below were taken
with `grep` over the non-test Go sources and the embedded migrations at commit
`4fafe6a`, and they are reproducible:

```bash
cd backend
grep -rl "jackc/pgx\|jackc/pgconn" --include=*.go internal cmd | grep -v _test | wc -l   # 19
grep -ro "RETURNING"   --include=*.go internal cmd | grep -v _test | wc -l               # 64
grep -ro "ON CONFLICT" --include=*.go internal cmd | grep -v _test | wc -l               # 21
grep -ro "SKIP LOCKED" --include=*.go internal cmd | grep -v _test | wc -l               #  8
grep -ro "ILIKE"       --include=*.go internal cmd | grep -v _test | wc -l               # 26
grep -ro "::interval"  --include=*.go internal cmd | grep -v _test | wc -l               #  7
grep -ro "pg_advisory" --include=*.go internal cmd | grep -v _test | wc -l               #  1
ls internal/migrations/*.up.sql | wc -l                                                   # 16
grep -rl "gen_random_uuid" internal/migrations/*.up.sql | wc -l                          #  6
grep -rl "plpgsql\|CREATE TRIGGER" internal/migrations/*.up.sql | wc -l                   #  6
```

## Decision

**PostgreSQL remains the only supported datastore.** A MySQL/MariaDB adapter is
not implemented in this cycle, and the direction is deferred until the blockers
below are resolved by a dedicated stage — not by a driver swap.

This is not a claim that the database is "Postgres-shaped" in a way a shim could
hide. It is the opposite: the application is written *against* PostgreSQL
features at four levels, and each level has to be addressed separately.

### Blocker 1 — the driver, not the dialect

`internal/database` wraps `*pgxpool.Pool` directly (`database.go`), and the `Tx`
type is `pgx.Tx`. Nineteen non-test files import `jackc/pgx` or `jackc/pgconn`,
and several of them do more than pass a connection around:

- `internal/crm/search.go` and `internal/crm/crm.go` scan with `pgx.Row`.
- `internal/jobs/jobs.go` and `internal/events/delivery.go` use
  `errors.Is(err, pgx.ErrNoRows)` to mean "no row", which is a pgx sentinel, not
  `database/sql.ErrNoRows`.
- `internal/importx/writer.go` and `internal/importx/service.go` use
  `errors.Is(err, pgx.ErrNoRows)` to distinguish "no row" from a real error while
  resolving references during an import, which is again a pgx sentinel.

A `database/sql` + `go-sql-driver/mysql` adapter would therefore require rewriting
the *call sites*, not only the wrapper: every `pgx.Row`/`pgx.Rows` signature and
every `pgx.ErrNoRows` check (in `jobs`, `events`, `importx`, `authn`, `crm`, the
workflow store and the outbox) has to move to the `database/sql` equivalents
together, or half the codebase keeps importing pgx and the adapter is a fiction.

### Blocker 2 — SQL the server executes, not the client

These are not formatting differences; they change what the statement means:

| Idiom | Uses | MySQL/MariaDB equivalent | Cost |
|---|---|---|---|
| `INSERT ... RETURNING` | 64 | none (MariaDB 10.5+ has `RETURNING` for `INSERT`, but not `UPDATE`/`DELETE`; MySQL has none) | Every insert/update that returns a row becomes insert-then-select, inside the same transaction, and needs a generated-key or last-insert-id strategy per table |
| `ON CONFLICT ... DO UPDATE/NOTHING` | 21 | `INSERT ... ON DUPLICATE KEY UPDATE` / `INSERT IGNORE` | Different semantics: `ON DUPLICATE KEY UPDATE` fires on *any* unique key, not the named one, and `INSERT IGNORE` swallows every error, not only the conflict. Each call site needs its own translation and test |
| `FOR UPDATE SKIP LOCKED` | 8 | MySQL 8.0+ and MariaDB 10.6+ support it | This is the queue's correctness foundation (`jobs.Claim`, `ClaimDelivery`). It works, but the minimum versions become a hard floor and every claim query needs a MariaDB variant |
| `ILIKE` | 26 | `LIKE` with a case-insensitive collation, or `LOWER()` | Collation-dependent; the search behaviour would silently change with the server's default collation |
| `now() - $1::interval` | 7 | `DATE_SUB(now(), INTERVAL ? SECOND)` | Parameter binding differs; the lease and retry arithmetic is spread across the queue and delivery code |
| `pg_advisory_xact_lock` | 1 | `GET_LOCK`/`RELEASE_LOCK`, session-scoped | `EnqueuePeriodic`'s de-duplication depends on a *transaction-scoped* lock; the MySQL equivalent is session-scoped and needs explicit release, which is a different failure mode |

### Blocker 3 — schema and migrations

The 16 migrations are PostgreSQL DDL and are embedded in the binary. They use:

- `gen_random_uuid()` as a column default in six migrations — MySQL would need
  `UUID()` (MariaDB) or application-side generation (MySQL has no `UUID` default
  in all versions), and a `CHAR(36)`/`BINARY(16)` representation choice that
  changes every UUID column and index size.
- `CREATE TRIGGER` with `plpgsql` in six migrations — notably the append-only
  audit trigger. MariaDB has no `plpgsql`; the trigger must be rewritten in its
  dialect and re-verified, because "the audit log cannot be deleted" is a security
  property, not a convenience.
- Partial indexes (`CREATE INDEX ... WHERE`) in four migrations — unsupported in
  MySQL, so each becomes a functional or full index plus a query change.
- `jsonb` columns in seven migrations — `JSON` in MySQL/MariaDB, with different
  operators (`->>` vs `JSON_EXTRACT`), different indexing, and no `jsonb` equality.
- Array columns (`uuid[]`, `text[]`) in five migrations — MySQL has no array type;
  each becomes a join table or a JSON array, which is a data-model change.

There is no shared migration dialect today, and `migrate.Up` runs the embedded
PostgreSQL files directly. A second datastore needs a second migration set kept in
lockstep, not a translation layer.

### Blocker 4 — the worker and the queue

The queue's guarantees — `FOR UPDATE SKIP LOCKED` claims, `ON CONFLICT` de-dup,
the advisory lock in `EnqueuePeriodic`, `::interval` lease arithmetic — are the
worker's correctness foundation. A datastore port that leaves any of them
approximated would ship a queue that double-runs or strands jobs, which is worse
than not supporting the datastore at all.

## Consequences

- The `migratex` *source* side is a separate question from running ForgeCRM *on*
  MySQL, and it is now implemented: `internal/migratex/mysql.go` reads from a MySQL
  or MariaDB database, so a customer can migrate *from* MySQL. That does not move
  this ADR's decision — reading a foreign database over `database/sql` exercises
  none of the blockers below, which are all about ForgeCRM's *own* schema, queries
  and queue running on the datastore. The datastore direction keeps the posture of
  honest refusal, not partial support.
- A future MySQL/MariaDB stage must, in order: (1) introduce a `database/sql`
  seam with a per-dialect statement builder and a `database.ErrNoRows` sentinel to
  replace the pgx one; (2) replace the COPY importer path; (3) author and verify a
  full second migration set, including the audit trigger in the target dialect;
  (4) run the queue and delivery claim tests against a real MariaDB before the
  datastore is claimed as supported.
- Compilation against a MySQL driver is explicitly **not** evidence of support.
  The acceptance bar is the integration suite — tenant isolation, the queue's
  claim behaviour, and the append-only audit trigger — passing on a real server.

## Status of the code

No MySQL/MariaDB *datastore* code is added by this ADR. It records the measured
blockers so that a later stage starts from facts, and so that the roadmap does not
carry the direction as "nearly done" when it is not started. The MySQL/MariaDB
*migration source* (`internal/migratex/mysql.go`) is a distinct, verified
capability — see roadmap row 2.6 — and does not depend on any of the blockers here.
