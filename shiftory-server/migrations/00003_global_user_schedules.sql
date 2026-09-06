-- +goose Up
-- Reset schedule/import business data; personal schedules are unique per user/date.
DELETE FROM schedule_revisions;
DELETE FROM schedule_segments;
DELETE FROM schedule_days;
DELETE FROM import_items;
DELETE FROM import_files;
DELETE FROM import_jobs;
ALTER TABLE schedule_days DROP INDEX uq_schedule_day;
ALTER TABLE schedule_days ADD UNIQUE KEY uq_schedule_day_user_date (user_id, work_date);

-- +goose Down
ALTER TABLE schedule_days DROP INDEX uq_schedule_day_user_date;
ALTER TABLE schedule_days ADD UNIQUE KEY uq_schedule_day (workspace_id, user_id, work_date);
