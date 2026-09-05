package importjob

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"shiftory-server/internal/schedule"
)

type MySQLRepository struct{ db *sql.DB }

func NewMySQLRepository(db *sql.DB) *MySQLRepository { return &MySQLRepository{db: db} }

func (r *MySQLRepository) Claim(ctx context.Context, workerID string, lease time.Duration) (*Job, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	var job Job
	var start, end string
	var instructions sql.NullString
	var mappingJSON []byte
	err = tx.QueryRowContext(ctx, `
SELECT j.id, j.workspace_id, j.upload_user_id, j.target_user_id,
       DATE_FORMAT(j.period_start, '%Y-%m-%d'), DATE_FORMAT(j.period_end, '%Y-%m-%d'),
       j.source_filename, f.storage_key, f.media_type, j.recognition_instructions, j.mapping_hints,
       j.attempt_count, j.max_attempts
FROM import_jobs j JOIN import_files f ON f.import_job_id = j.id
WHERE j.import_type = 'IMAGE_AI'
  AND ((j.state = 'PENDING' AND (j.retry_not_before IS NULL OR j.retry_not_before <= UTC_TIMESTAMP(6)))
       OR (j.state = 'PARSING' AND j.lease_expires_at < UTC_TIMESTAMP(6)))
ORDER BY j.created_at, j.id LIMIT 1 FOR UPDATE SKIP LOCKED`).Scan(
		&job.ID, &job.WorkspaceID, &job.UploadUserID, &job.TargetUserID, &start, &end, &job.SourceFilename,
		&job.StorageKey, &job.MediaType, &instructions, &mappingJSON, &job.AttemptCount, &job.MaxAttempts)
	if errors.Is(err, sql.ErrNoRows) {
		if err := tx.Commit(); err != nil {
			return nil, err
		}
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	job.PeriodStart, job.PeriodEnd = schedule.MustDate(start), schedule.MustDate(end)
	job.RecognitionInstructions = instructions.String
	job.MappingHints = map[string]string{}
	if len(mappingJSON) > 0 {
		if err := json.Unmarshal(mappingJSON, &job.MappingHints); err != nil {
			return nil, fmt.Errorf("decode job mapping hints: %w", err)
		}
	}
	job.AttemptCount++
	job.LeaseOwner = workerID
	result, err := tx.ExecContext(ctx, `
UPDATE import_jobs
SET state = 'PARSING', attempt_count = ?, lease_owner = ?, lease_expires_at = ?, heartbeat_at = UTC_TIMESTAMP(6), retry_not_before = NULL,
    error_code = NULL, error_message = NULL
WHERE id = ?`, job.AttemptCount, workerID, time.Now().UTC().Add(lease), job.ID)
	if err != nil {
		return nil, err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return nil, errors.New("claimed import job disappeared")
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &job, nil
}

func (r *MySQLRepository) Heartbeat(ctx context.Context, jobID uint64, workerID string, lease time.Duration) error {
	result, err := r.db.ExecContext(ctx, `
UPDATE import_jobs SET heartbeat_at = UTC_TIMESTAMP(6), lease_expires_at = ?
WHERE id = ? AND state = 'PARSING' AND lease_owner = ?`, time.Now().UTC().Add(lease), jobID, workerID)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("worker no longer owns import job lease")
	}
	return nil
}

func (r *MySQLRepository) Complete(ctx context.Context, job Job, result Result) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var state, leaseOwner string
	if err := tx.QueryRowContext(ctx, `SELECT state, COALESCE(lease_owner, '') FROM import_jobs WHERE id = ? FOR UPDATE`, job.ID).Scan(&state, &leaseOwner); err != nil {
		return err
	}
	if state == "NEEDS_REVIEW" {
		return tx.Commit()
	}
	if state != "PARSING" || leaseOwner != job.LeaseOwner {
		return errors.New("worker no longer owns import job")
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM import_items WHERE import_job_id = ?`, job.ID); err != nil {
		return err
	}
	for _, item := range result.Items {
		var draft, issues any
		if len(item.DraftSnapshot) > 0 {
			draft = item.DraftSnapshot
		}
		if len(item.Issues) > 0 {
			issues = item.Issues
		}
		if _, err := tx.ExecContext(ctx, `
INSERT INTO import_items
    (import_job_id, work_date, item_type, draft_snapshot, existing_schedule_id, existing_version, issues, error_message, sort_order)
VALUES (?, ?, ?, ?, ?, ?, ?, NULLIF(?, ''), ?)`, job.ID, item.Date.String(), item.Type, draft, item.ExistingScheduleID,
			item.ExistingVersion, issues, item.ErrorMessage, item.SortOrder); err != nil {
			return err
		}
	}
	var raw any
	if len(result.RawResponse) > 0 {
		raw = result.RawResponse
	}
	update, err := tx.ExecContext(ctx, `
UPDATE import_jobs
SET state = 'NEEDS_REVIEW', item_count = ?, conflict_count = ?, invalid_count = ?, model_name = ?, prompt_version = ?, schema_version = ?,
    ai_raw_response = ?, lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL
WHERE id = ? AND state = 'PARSING' AND lease_owner = ?`, result.ItemCount, result.ConflictCount, result.InvalidCount,
		result.ModelName, result.PromptVersion, result.SchemaVersion, raw, job.ID, job.LeaseOwner)
	if err != nil {
		return err
	}
	if affected, _ := update.RowsAffected(); affected != 1 {
		return errors.New("worker lost import job before completion")
	}
	return tx.Commit()
}

func (r *MySQLRepository) Retry(ctx context.Context, job Job, processErr error, next time.Time) error {
	return r.finishAttempt(ctx, job, "PENDING", "RETRYABLE_AI_ERROR", processErr, &next)
}

func (r *MySQLRepository) Fail(ctx context.Context, job Job, processErr error) error {
	return r.finishAttempt(ctx, job, "FAILED", "AI_PROCESSING_FAILED", processErr, nil)
}

func (r *MySQLRepository) finishAttempt(ctx context.Context, job Job, state, code string, processErr error, retryAt *time.Time) error {
	message := strings.TrimSpace(processErr.Error())
	if len(message) > 4000 {
		message = message[:4000]
	}
	result, err := r.db.ExecContext(ctx, `
UPDATE import_jobs
SET state = ?, error_code = ?, error_message = ?, retry_not_before = ?, lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL
WHERE id = ? AND state = 'PARSING' AND lease_owner = ?`, state, code, message, retryAt, job.ID, job.LeaseOwner)
	if err != nil {
		return err
	}
	if affected, _ := result.RowsAffected(); affected != 1 {
		return errors.New("worker no longer owns import job")
	}
	return nil
}
