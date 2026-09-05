-- +goose Up
ALTER TABLE import_jobs
    ADD COLUMN recognition_instructions TEXT NULL AFTER schema_version,
    ADD COLUMN mapping_hints JSON NULL AFTER recognition_instructions,
    ADD COLUMN ai_raw_response JSON NULL AFTER mapping_hints,
    ADD COLUMN retry_not_before DATETIME(6) NULL AFTER ai_raw_response;

-- +goose Down
ALTER TABLE import_jobs
    DROP COLUMN retry_not_before,
    DROP COLUMN ai_raw_response,
    DROP COLUMN mapping_hints,
    DROP COLUMN recognition_instructions;
