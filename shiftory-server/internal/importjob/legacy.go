package importjob

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"
)

// MigrateLegacy 仅在旧 API 已停机时固定历史输入并投递尚无 Outbox 的任务
func MigrateLegacy(ctx context.Context, db *sql.DB, maxRounds int) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT id,workspace_id,created_at FROM import_jobs j WHERE import_type='IMAGE_AI' AND state IN ('PENDING','PARSING') AND (lease_owner IS NULL OR lease_expires_at<statement_timestamp()) AND NOT EXISTS(SELECT 1 FROM import_outbox WHERE job_id=j.id) ORDER BY id`)
	if err != nil {
		return 0, err
	}
	type old struct {
		id, workspace uint64
		created       time.Time
	}
	jobs := []old{}
	for rows.Next() {
		var o old
		if err = rows.Scan(&o.id, &o.workspace, &o.created); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, o)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, err
	}
	count := 0
	for _, j := range jobs {
		snapshot, err := FreezeInput(ctx, db, j.workspace)
		if err != nil {
			return count, err
		}
		var frozen frozenInput
		if err = json.Unmarshal(snapshot, &frozen); err != nil {
			return count, err
		}
		frozen.FixedNow = j.created
		snapshot, _ = json.Marshal(frozen)
		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return count, err
		}
		var g uint64
		err = tx.QueryRowContext(ctx, `SELECT run_generation FROM import_jobs WHERE id=$1 AND state IN ('PENDING','PARSING') AND (lease_owner IS NULL OR lease_expires_at<statement_timestamp()) AND NOT EXISTS(SELECT 1 FROM import_outbox WHERE job_id=$2) FOR UPDATE`, j.id, j.id).Scan(&g)
		if err == sql.ErrNoRows {
			tx.Rollback()
			continue
		}
		if err != nil {
			tx.Rollback()
			return count, err
		}
		_, err = tx.ExecContext(ctx, `UPDATE import_jobs SET state='PENDING',stage='WAITING',run_generation=run_generation+1,attempt_count=0,max_attempts=$1,input_snapshot=$2,lease_owner=NULL,lease_expires_at=NULL,heartbeat_at=NULL,retry_not_before=NULL WHERE id=$3`, maxRounds, snapshot, j.id)
		if err == nil {
			err = AddOutbox(ctx, tx, j.id, g+1)
		}
		if err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}
