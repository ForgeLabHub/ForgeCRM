# ADR 0012 — Audit writer and hash-chain verifier

- **Status:** Proposed (plan; the writer exists, the verifier does not)
- **Date:** 2026-10-29
- **Deciders:** project owner, engineering agent
- **Depends on:** ADR 0009 (security as independent building blocks)

## Context

`internal/audit` already writes two streams:

- `audit_log` — the action trail, append-only **enforced by a PostgreSQL trigger**
  (`audit_log_immutable`) that raises on `UPDATE`/`DELETE`. It records actor,
  action, entity, before/after JSON, metadata, correlation id, IP and time, with
  indexes on `(organization_id, created_at)`, `(actor_user_id, created_at)`,
  `(entity_type, entity_id)` and `(action, created_at)`.
- `security_events` — authentication and security events.

The trail is append-only against the **application**. It is not tamper-evident
against anyone who can run DDL: such a person can disable the trigger, rewrite a
row, and re-enable it, leaving no trace. A verifier over a cryptographic hash
chain closes that gap for *detection* — it cannot prevent the rewrite, but it
makes a silent rewrite detectable.

The current writer uses the ambient transaction (`database.WithTx`), which is what
makes an audited write atomic with the write it describes. A verifier must not
break that property.

## Proposed contract

```go
type Event struct {
	ID          string
	Actor       string
	Action      string
	Resource    string
	PayloadHash []byte
	PrevHash    []byte
	Hash        []byte
	At          time.Time
}

type Writer interface {
	Append(ctx context.Context, event Event) error
}

type Verifier interface {
	Verify(ctx context.Context, from, to time.Time) (VerifyResult, error)
}
```

The event model must be checked against the existing `audit.Entry` and the
`audit_log` columns before implementation. The chain adds `payload_hash`,
`prev_hash` and `hash`; the existing columns are not replaced. This is an
extension of the current writer, **not a parallel logging system** — a second
audit path would be worse than none, because it would split the source of truth.

## Implementations

- `PostgresWriter` — the existing writer, extended to compute and store the chain
  fields in the same transaction as the audited write.
- `MemoryWriter` — an in-process writer for tests and demo; deterministic.
- `HashChainVerifier` — walks a range, recomputes each `hash` from the record and
  `prev_hash`, and reports the first inconsistency.

## Hash chain

Each record's `hash = SHA-256(canonical(record) || prev_hash)`. `prev_hash` is the
previous record's `hash`. The genesis record's `prev_hash` is a defined constant.

The verifier checks:

- sequence continuity (no gap);
- each record's `prev_hash` equals the previous record's `hash`;
- each record's recomputed `hash` equals its stored `hash`;
- `payload_hash` matches the payload;
- the time range requested.

A mismatch at any point identifies the first altered or reordered record.

**Concurrency.** Parallel `Append` calls must not produce a fork. The chain is
serialised — a per-organisation (or per-stream) lock, or a database-level
serialising mechanism — so two writers cannot both extend the same `prev_hash`.
The exact mechanism is an implementation decision, but it must be tested under
`-race` with concurrent appends.

## Limitation that must be documented in the code

A hash chain **does not make the log immutable**. An attacker with write access to
the chain and its records can recompute the entire chain from the point of
tampering onward, producing a chain that verifies. The chain detects an *isolated*
edit; it does not defeat an attacker who can rewrite the whole chain.

To strengthen this, **external checkpoints** are needed: the head `hash` published
to a store the attacker does not control (a separate service, a signed
timestamp, an append-only external log). This ADR **plans the seam for
checkpoints but does not claim they are implemented.** The verifier should expose
the head hash so a future checkpoint writer can consume it.

## Required tests

- the first record (genesis) verifies;
- several consecutive records verify;
- an altered payload is detected;
- an altered `hash` is detected;
- a deleted record is detected (gap);
- two reordered records are detected;
- concurrent appends produce a single valid chain;
- an empty range verifies as valid-and-empty, not as an error;
- an inverted range (from > to) is an explicit error;
- a transaction error leaves no partial chain entry.

## Extract boundary

`Writer`, `Verifier`, `MemoryWriter` and `HashChainVerifier` move as a unit and
build with no CRM dependency. `PostgresWriter` needs the `database` package and
the `audit_log` schema. The extract-test builds the chain with `MemoryWriter` and
runs the tamper tests without PostgreSQL.

## Revision trigger

Revisit when external checkpoints are implemented, when the writer's transaction
coupling with `database.WithTx` proves too tight for extraction, or if a second
stream (security events) needs its own chain.
