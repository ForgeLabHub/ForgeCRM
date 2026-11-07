-- Private conversation history is owned by a user within an organization.
-- The composite foreign key makes it impossible to attach a message to a
-- conversation with a different owner or tenant, even through a faulty writer.
CREATE TABLE ai_conversations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
    user_id uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    title text NOT NULL CHECK (length(btrim(title)) BETWEEN 1 AND 200),
    pinned boolean NOT NULL DEFAULT false,
    archived_at timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (id, organization_id, user_id)
);
CREATE INDEX ai_conversations_owner_idx ON ai_conversations
    (organization_id, user_id, pinned DESC, updated_at DESC, id DESC);

CREATE TABLE ai_messages (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    organization_id uuid NOT NULL,
    user_id uuid NOT NULL,
    conversation_id uuid NOT NULL,
    role text NOT NULL CHECK (role IN ('user', 'assistant')),
    content text NOT NULL CHECK (length(btrim(content)) BETWEEN 1 AND 16000),
    created_at timestamptz NOT NULL DEFAULT now(),
    FOREIGN KEY (conversation_id, organization_id, user_id)
        REFERENCES ai_conversations (id, organization_id, user_id) ON DELETE CASCADE
);
CREATE INDEX ai_messages_conversation_idx ON ai_messages
    (organization_id, user_id, conversation_id, created_at, id);
