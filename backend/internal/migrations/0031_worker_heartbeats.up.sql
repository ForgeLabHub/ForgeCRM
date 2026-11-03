-- Worker liveness.
--
-- The API has a readiness probe; the worker has nothing. On the deployment this
-- project targets the worker is a separate service with no health check, so a
-- worker that has exited, crashed on a bad job, or deadlocked holds its database
-- connection, appears healthy to the platform, and stops draining the queue. The
-- only symptom is a `jobs` table that grows, and the operations guide says to look
-- there — after someone has noticed. Nothing answers "is the worker up right now".
--
-- This table is that answer. Each worker process upserts one row on a fixed
-- interval, and a row whose `last_seen_at` is recent means a live worker. The API
-- reads the table and reports the count as a gauge, so a scrape can alert on it
-- and a dashboard can show it next to the queue depth.
--
-- A heartbeat rather than a status flag, deliberately: a flag set to "running" at
-- startup and cleared at shutdown cannot distinguish a running worker from one
-- that died without running its shutdown path, which is the case that matters. A
-- timestamp that must keep advancing ages out on its own, so a crashed worker
-- disappears from the count without anyone clearing anything.
--
-- `worker_id` is the primary key, so the table holds one row per worker process
-- and several replicas coexist. The value is the hostname where one is available
-- and a random string otherwise, which is what the worker already uses to label
-- its job claims.
--
-- `role` is stored because the same image runs both binaries; a heartbeat that
-- said only "something is alive" could be an API instance that never drains a
-- queue. `version` and `commit` are stored so an operator can see which build is
-- running without opening the worker's log.

CREATE TABLE worker_heartbeats (
    worker_id    text PRIMARY KEY,
    role         text NOT NULL,
    version      text NOT NULL DEFAULT '',
    commit       text NOT NULL DEFAULT '',
    started_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

-- The liveness count is `WHERE last_seen_at > now() - interval`, so the index is
-- on that column. Without it the count scans the table; the table is tiny, but the
-- index also documents which query the table exists to serve.
CREATE INDEX worker_heartbeats_last_seen_idx ON worker_heartbeats (last_seen_at);
