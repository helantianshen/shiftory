package importjob

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
	"shiftory-server/internal/schedule"
	"shiftory-server/internal/testutil"
)

// TestPostgresRepositoryRecoversExpiredLeaseAndCompletesAtomically 验证过期租约可领取及任务预览原子完成
func TestPostgresRepositoryRecoversExpiredLeaseAndCompletesAtomically(t *testing.T) {
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PARSING", time.Now().UTC().Add(-time.Minute))
	repository := NewPostgresRepository(db)
	cfg, _ := config.Load()
	q := &Queue{db: db, cfg: cfg}
	job, err := q.claim(context.Background(), Message{1, 1, 1})
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job == nil || job.AttemptCount != 2 || job.StorageKey != "imports/test.png" {
		t.Fatalf("unexpected claimed job: %+v", job)
	}
	result := Result{Items: []ResultItem{{Date: schedule.MustDate("2026-09-01"), Type: "UNCERTAIN", Issues: []byte(`[]`)}}, ItemCount: 1, ModelName: "vision", PromptVersion: "p1", SchemaVersion: "s1", RawResponse: []byte(`{"entries":[]}`)}
	if err := repository.Complete(context.Background(), *job, result); err != nil {
		t.Fatalf("complete: %v", err)
	}
	var state string
	var itemCount int
	if err := db.QueryRow(`SELECT state, item_count FROM import_jobs WHERE id = $1`, job.ID).Scan(&state, &itemCount); err != nil {
		t.Fatal(err)
	}
	if state != "NEEDS_REVIEW" || itemCount != 1 {
		t.Fatalf("unexpected completion state=%s itemCount=%d", state, itemCount)
	}
}

// openWorkerTestDatabase 连接并清理 Worker 专用测试数据库，注册连接清理回调
func openWorkerTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	dsn := testutil.PostgresDSN(t, "shiftory_test_importjob")
	db, err := database.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec("TRUNCATE TABLE users, workspaces, workspace_members, shifts, shift_aliases, import_jobs, import_files, schedule_days, schedule_segments, schedule_revisions, import_items, import_outbox, import_attempts, import_provider_calls RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	return db
}

// seedWorkerJob 插入工作区、用户和指定状态的图片任务作为测试数据
func seedWorkerJob(t *testing.T, db *sql.DB, state string, leaseExpires time.Time) {
	t.Helper()
	var userID int64
	err := db.QueryRow(`INSERT INTO users (username, username_normalized, email, email_normalized, display_name, password_hash) VALUES ('worker-user', 'worker-user', 'worker@example.com', 'worker@example.com', 'Worker', 'hash') RETURNING id`).Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}

	var workspaceID int64
	err = db.QueryRow(`INSERT INTO workspaces (name, timezone, owner_user_id, created_by) VALUES ('Worker test', 'Asia/Shanghai', $1, $2) RETURNING id`, userID, userID).Scan(&workspaceID)
	if err != nil {
		t.Fatal(err)
	}

	var jobID int64
	err = db.QueryRow(`
INSERT INTO import_jobs (workspace_id, upload_user_id, target_user_id, import_type, state, period_start, period_end, source_filename, idempotency_key, attempt_count, lease_owner, lease_expires_at)
VALUES ($1, $2, $3, 'IMAGE_AI', $4, '2026-09-01', '2026-09-30', 'test.png', 'worker-test', 1, 'dead-worker', $5) RETURNING id`, workspaceID, userID, userID, state, leaseExpires).Scan(&jobID)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := db.Exec(`INSERT INTO import_files (import_job_id, storage_key, original_name, media_type, byte_size, sha256) VALUES ($1, 'imports/test.png', 'test.png', 'image/png', 3, $2)`, jobID, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
}
