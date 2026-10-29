-- +goose Up
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE TABLE users (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email text NOT NULL,
    email_normalized text GENERATED ALWAYS AS (lower(btrim(email))) STORED,
    display_name text NOT NULL,
    account_status text NOT NULL DEFAULT 'active',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_normalized_length CHECK (char_length(email_normalized) BETWEEN 3 AND 320),
    CONSTRAINT users_display_name_length CHECK (char_length(btrim(display_name)) BETWEEN 1 AND 80),
    CONSTRAINT users_account_status CHECK (account_status IN ('active', 'suspended', 'deleted'))
);

CREATE UNIQUE INDEX users_email_normalized_unique ON users (email_normalized);

CREATE TABLE locations (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    location_type text NOT NULL,
    provider text,
    provider_place_id text,
    display_label text NOT NULL,
    coordinates geography(Point, 4326),
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT locations_type CHECK (
        location_type IN ('verified_place', 'unverified_place_reference', 'broad_area', 'coordinate_only')
    ),
    CONSTRAINT locations_display_label_length CHECK (char_length(btrim(display_label)) BETWEEN 1 AND 200),
    CONSTRAINT locations_provider_pair CHECK ((provider IS NULL) = (provider_place_id IS NULL)),
    CONSTRAINT locations_verified_provider CHECK (
        (location_type = 'verified_place') = (provider IS NOT NULL AND provider_place_id IS NOT NULL)
    ),
    CONSTRAINT locations_coordinate_kind CHECK (
        (location_type = 'coordinate_only' AND coordinates IS NOT NULL)
        OR (location_type IN ('verified_place', 'unverified_place_reference', 'broad_area')
            AND (location_type = 'verified_place' OR coordinates IS NULL))
    )
);

CREATE UNIQUE INDEX locations_provider_place_unique
    ON locations (provider, provider_place_id)
    WHERE provider IS NOT NULL AND provider_place_id IS NOT NULL;
CREATE INDEX locations_coordinates_gist
    ON locations USING gist (coordinates)
    WHERE coordinates IS NOT NULL;

CREATE TABLE notes (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    author_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    location_id uuid NOT NULL REFERENCES locations (id) ON DELETE RESTRICT,
    title text NOT NULL,
    body text NOT NULL,
    category text NOT NULL,
    status text NOT NULL DEFAULT 'published',
    created_at timestamptz NOT NULL DEFAULT now(),
    updated_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT notes_title_length CHECK (char_length(btrim(title)) BETWEEN 3 AND 120),
    CONSTRAINT notes_body_length CHECK (char_length(btrim(body)) BETWEEN 1 AND 5000),
    CONSTRAINT notes_category CHECK (category IN ('hidden_gem', 'shortcut', 'local_tip', 'parking', 'warning')),
    CONSTRAINT notes_status CHECK (status IN ('published', 'hidden', 'removed'))
);

CREATE INDEX notes_published_by_location
    ON notes (location_id, created_at DESC)
    WHERE status = 'published';
CREATE INDEX notes_published_by_author
    ON notes (author_user_id, created_at DESC)
    WHERE status = 'published';
CREATE INDEX notes_published_by_category
    ON notes (category, created_at DESC)
    WHERE status = 'published';

CREATE TABLE votes (
    note_id uuid NOT NULL REFERENCES notes (id) ON DELETE RESTRICT,
    user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    vote_type text NOT NULL DEFAULT 'helpful',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT votes_one_active_per_user_note PRIMARY KEY (note_id, user_id),
    CONSTRAINT votes_type CHECK (vote_type IN ('helpful'))
);

CREATE INDEX votes_by_user ON votes (user_id, created_at DESC);

CREATE TABLE reports (
    id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    reporter_user_id uuid NOT NULL REFERENCES users (id) ON DELETE RESTRICT,
    reported_note_id uuid NOT NULL REFERENCES notes (id) ON DELETE RESTRICT,
    reason text NOT NULL,
    status text NOT NULL DEFAULT 'open',
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT reports_reason_length CHECK (char_length(btrim(reason)) BETWEEN 1 AND 1000),
    CONSTRAINT reports_status CHECK (status IN ('open', 'reviewing', 'resolved', 'dismissed', 'actioned'))
);

CREATE UNIQUE INDEX reports_one_active_per_user_note
    ON reports (reporter_user_id, reported_note_id)
    WHERE status IN ('open', 'reviewing');
CREATE INDEX reports_by_note_and_status
    ON reports (reported_note_id, status, created_at DESC);

-- +goose StatementBegin
CREATE FUNCTION set_updated_at() RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER users_set_updated_at
    BEFORE UPDATE ON users
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
CREATE TRIGGER notes_set_updated_at
    BEFORE UPDATE ON notes
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();

-- +goose Down
DROP TABLE reports;
DROP TABLE votes;
DROP TABLE notes;
DROP TABLE locations;
DROP TABLE users;
DROP FUNCTION set_updated_at();
