package importjob

import (
	"context"
	"database/sql"
	"errors"
)

// PostgresRepository 使用共享 Postgres 连接池实现本模块的持久化操作
type PostgresRepository struct{ db *sql.DB }

// NewPostgresRepository 使用调用方管理的数据库连接池创建图片任务仓储
func NewPostgresRepository(db *sql.DB) *PostgresRepository { return &PostgresRepository{db: db} }

// Complete 在同一事务中保存预览项并进入待审核状态，已待审核任务按幂等完成处理
func (r *PostgresRepository) Complete(ctx context.Context, job Job, result Result) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var state, leaseOwner string
	var generation uint64
	var valid bool
	if err := tx.QueryRowContext(ctx, `SELECT state, COALESCE(lease_owner, ''),run_generation,COALESCE(lease_expires_at>statement_timestamp(),FALSE) FROM import_jobs WHERE id = $1 FOR UPDATE`, job.ID).Scan(&state, &leaseOwner, &generation, &valid); err != nil {
		return err
	}
	if state == "NEEDS_REVIEW" {
		// 已写入预览的任务视为幂等完成，重复完成不得覆盖人工修改
		return tx.Commit()
	}
	if state != "PARSING" || leaseOwner != job.LeaseOwner || !valid || generation != job.Generation {
		return errors.New("worker no longer owns import job")
	}
	// 获得任务所有权后整体替换预览项，与任务进入待审核状态一起提交
	if _, err := tx.ExecContext(ctx, `DELETE FROM import_items WHERE import_job_id = $1`, job.ID); err != nil {
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
VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9)`, job.ID, item.Date.String(), item.Type, draft, item.ExistingScheduleID,
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
SET state = 'NEEDS_REVIEW', item_count = $1, conflict_count = $2, invalid_count = $3, model_name = $4, prompt_version = $5, schema_version = $6,
    ai_raw_response = $7,rule_snapshot=$8,job_issues=$9,review_version=review_version+1,stage='REVIEW', lease_owner = NULL, lease_expires_at = NULL, heartbeat_at = NULL
WHERE id = $10 AND state = 'PARSING' AND lease_owner = $11`, result.ItemCount, result.ConflictCount, result.InvalidCount,
		result.ModelName, result.PromptVersion, result.SchemaVersion, raw, nullableJSON(result.RulesSnapshot), nullableJSON(result.JobIssues), job.ID, job.LeaseOwner)
	if err != nil {
		return err
	}
	if affected, _ := update.RowsAffected(); affected != 1 {
		return errors.New("worker lost import job before completion")
	}
	if job.AttemptID > 0 {
		if _, err := tx.ExecContext(ctx, `UPDATE import_attempts SET state='SUCCESS',finished_at=statement_timestamp() WHERE id=$1 AND job_id=$2 AND generation=$3 AND state='RUNNING'`, job.AttemptID, job.ID, job.Generation); err != nil {
			return err
		}
	}
	return tx.Commit()
}
