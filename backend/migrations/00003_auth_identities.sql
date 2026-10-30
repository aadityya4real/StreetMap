-- +goose Up
ALTER TABLE users ALTER COLUMN email DROP NOT NULL;

CREATE TABLE auth_identities (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    issuer text NOT NULL,
    subject text NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_identities_issuer_nonempty CHECK (char_length(btrim(issuer)) > 0),
    CONSTRAINT auth_identities_subject_nonempty CHECK (char_length(btrim(subject)) > 0),
    CONSTRAINT auth_identities_issuer_subject_unique UNIQUE (issuer, subject)
);

CREATE INDEX auth_identities_by_user ON auth_identities (user_id);

-- +goose Down
-- +goose StatementBegin
DO $$
BEGIN
    LOCK TABLE users IN ACCESS EXCLUSIVE MODE;
    IF EXISTS (SELECT 1 FROM users WHERE email IS NULL) THEN
        RAISE EXCEPTION 'cannot restore users.email NOT NULL: null email values exist; resolve them explicitly before retrying rollback';
    END IF;
END;
$$;
-- +goose StatementEnd

DROP TABLE auth_identities;
ALTER TABLE users ALTER COLUMN email SET NOT NULL;
