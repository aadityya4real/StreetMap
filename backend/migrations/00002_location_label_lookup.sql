-- +goose Up
CREATE INDEX locations_by_type_and_normalized_label
    ON locations (location_type, lower(btrim(display_label)));

-- +goose Down
DROP INDEX locations_by_type_and_normalized_label;
