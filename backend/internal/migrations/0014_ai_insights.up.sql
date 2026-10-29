-- 0014_ai_insights: cached contextual AI results, keyed by the entity and by a
-- hash of the context that produced them.
--
-- Why a cache table rather than an in-process map:
--
--   * A model completion costs money and a round trip. The same summary is asked
--     for every time a card is opened, and the underlying record changes rarely.
--     Without a store, every page view is a new billable call.
--   * The cache must survive a restart and be shared between replicas, because
--     the API runs as more than one process. An in-memory map would be per
--     replica and would double the spend of a two-replica deployment.
--   * The key includes a hash of the assembled context, not just the entity id.
--     A record whose fields changed produces a different hash and therefore a
--     cache miss, so a stale answer is never served as if it were current.
--
-- entity_id has no foreign key on purpose: it names a row in one of several
-- tables (accounts, contacts, deals, tickets, tasks) depending on entity_type,
-- and PostgreSQL cannot express that. The consequence is that archiving or
-- deleting a record leaves its cached insight behind until it expires. That is
-- acceptable because the row holds a derived summary that the sweep removes and
-- because nothing joins through it; the alternative — five nullable FK columns
-- with a CHECK that exactly one is set — would make every query worse to protect
-- a value that is discarded anyway.
--
-- Nothing here is a second copy of customer data: `facts` holds the labels and
-- values the server itself rendered into the prompt, which is what makes it
-- possible to show the user exactly which facts the model was given.

CREATE TABLE ai_insights (
    id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    entity_type     text NOT NULL
                    CHECK (entity_type IN ('account', 'contact', 'deal', 'ticket', 'task')),
    entity_id       uuid NOT NULL,
    kind            text NOT NULL
                    CHECK (kind IN ('summary', 'risks', 'next_actions', 'draft', 'wrap_up', 'ask')),
    -- context_hash is a hash of the facts and the question. Two requests with the
    -- same hash have the same input, so the stored answer is the answer the model
    -- would produce again.
    context_hash    text NOT NULL,
    provider        text NOT NULL,
    model           text NOT NULL,
    facts           jsonb NOT NULL DEFAULT '[]'::jsonb,
    analysis        jsonb NOT NULL DEFAULT '{}'::jsonb,
    disclosures     jsonb NOT NULL DEFAULT '[]'::jsonb,
    input_tokens    integer,
    output_tokens   integer,
    created_by      uuid REFERENCES users(id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    expires_at      timestamptz NOT NULL,
    UNIQUE (organization_id, entity_type, entity_id, kind, context_hash)
);

-- The read path is always "the newest unexpired row for this entity and kind".
CREATE INDEX ai_insights_lookup_idx
    ON ai_insights (organization_id, entity_type, entity_id, kind, created_at DESC);

-- The retention sweep deletes by expiry, so it needs its own index rather than
-- scanning the table.
CREATE INDEX ai_insights_expiry_idx ON ai_insights (expires_at);
