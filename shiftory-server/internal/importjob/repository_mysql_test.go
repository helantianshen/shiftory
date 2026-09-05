package importjob

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"shiftory-server/internal/platform/database"
	"shiftory-server/internal/schedule"
)

func TestMySQLRepositoryRecoversExpiredLeaseAndCompletesAtomically(t *testing.T) {
	db := openWorkerTestDatabase(t)
	seedWorkerJob(t, db, "PARSING", time.Now().UTC().Add(-time.Minute))
	repository := NewMySQLRepository(db)
	job, err := repository.Claim(context.Background(), "worker-new", time.Minute)
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
	if err := db.QueryRow(`SELECT state, item_count FROM import_jobs WHERE id = ?`, job.ID).Scan(&state, &itemCount); err != nil {
		t.Fatal(err)
	}
	if state != "NEEDS_REVIEW" || itemCount != 1 {
		t.Fatalf("unexpected completion state=%s itemCount=%d", state, itemCount)
	}
}

func openWorkerTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	admin, err := database.Open(context.Background(), "root:123456@tcp(127.0.0.1:3306)/mysql?charset=utf8mb4&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("open MySQL for test database creation: %v", err)
	}
	if _, err := admin.Exec(`CREATE DATABASE IF NOT EXISTS shiftory_test_importjob CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci`); err != nil {
		_ = admin.Close()
		t.Fatalf("create isolated test database: %v", err)
	}
	_ = admin.Close()
	db, err := database.Open(context.Background(), "root:123456@tcp(127.0.0.1:3306)/shiftory_test_importjob?charset=utf8mb4&parseTime=true&loc=UTC")
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if _, err := db.Exec(`SET FOREIGN_KEY_CHECKS=0`); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"import_items", "schedule_revisions", "schedule_segments", "schedule_days", "import_files", "import_jobs", "shift_aliases", "shifts", "workspace_members", "workspaces", "users"} {
		if _, err := db.Exec("TRUNCATE TABLE " + table); err != nil {
			t.Fatal(err)
		}
	}
	_, _ = db.Exec(`SET FOREIGN_KEY_CHECKS=1`)
	return db
}

func seedWorkerJob(t *testing.T, db *sql.DB, state string, leaseExpires time.Time) {
	t.Helper()
	result, err := db.Exec(`INSERT INTO users (username, username_normalized, email, email_normalized, display_name, password_hash) VALUES ('worker-user', 'worker-user', 'worker@example.com', 'worker@example.com', 'Worker', 'hash')`)
	if err != nil {
		t.Fatal(err)
	}
	userID, _ := result.LastInsertId()
	result, err = db.Exec(`INSERT INTO workspaces (name, timezone, owner_user_id, created_by) VALUES ('Worker test', 'Asia/Shanghai', ?, ?)`, userID, userID)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, _ := result.LastInsertId()
	result, err = db.Exec(`
INSERT INTO import_jobs (workspace_id, upload_user_id, target_user_id, import_type, state, period_start, period_end, source_filename, idempotency_key, attempt_count, lease_owner, lease_expires_at)
VALUES (?, ?, ?, 'IMAGE_AI', ?, '2026-09-01', '2026-09-30', 'test.png', 'worker-test', 1, 'dead-worker', ?)`, workspaceID, userID, userID, state, leaseExpires)
	if err != nil {
		t.Fatal(err)
	}
	jobID, _ := result.LastInsertId()
	if _, err := db.Exec(`INSERT INTO import_files (import_job_id, storage_key, original_name, media_type, byte_size, sha256) VALUES (?, 'imports/test.png', 'test.png', 'image/png', 3, ?)`, jobID, make([]byte, 32)); err != nil {
		t.Fatal(err)
	}
}
