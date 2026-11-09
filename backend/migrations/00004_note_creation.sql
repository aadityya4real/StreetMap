-- +goose Up
ALTER TABLE notes DROP CONSTRAINT notes_category;
ALTER TABLE notes ADD CONSTRAINT notes_category CHECK (
    category IN (
        'hidden_gem', 'shortcut', 'local_tip', 'parking', 'warning',
        'road_condition', 'safety', 'other'
    )
);

CREATE TABLE note_creation_requests (
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    idempotency_key uuid NOT NULL,
    request_hash bytea NOT NULL,
    note_id uuid NOT NULL UNIQUE REFERENCES notes (id) ON DELETE RESTRICT,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT note_creation_requests_payload_hash_length CHECK (octet_length(request_hash) = 32),
    CONSTRAINT note_creation_requests_user_key_unique UNIQUE (user_id, idempotency_key)
);

-- +goose Down
-- Rollback is allowed only while every stored note still uses a category supported
-- by migration 00001. Refuse before dropping the idempotency ledger so rollback
-- cannot silently strand newly categorized notes under an invalid old constraint.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (
        SELECT 1 FROM notes
        WHERE category NOT IN ('hidden_gem', 'shortcut', 'local_tip', 'parking', 'warning')
    ) THEN
        RAISE EXCEPTION 'cannot roll back note categories while notes use road_condition, safety, or other';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TABLE note_creation_requests;
ALTER TABLE notes DROP CONSTRAINT notes_category;
ALTER TABLE notes ADD CONSTRAINT notes_category CHECK (
    category IN ('hidden_gem', 'shortcut', 'local_tip', 'parking', 'warning')
);
