package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestMigrateCreatesCompleteSchema(t *testing.T) {
	dsn := os.Getenv("SHIFTORY_TEST_DATABASE_DSN")
	if dsn == "" {
		dsn = "root:123456@tcp(127.0.0.1:3306)/shiftory_test?charset=utf8mb4&parseTime=true&loc=UTC"
	}
	ensureTestDatabase(t, dsn)

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(context.Background()); err != nil {
		t.Fatalf("ping test database: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("second migration must be idempotent: %v", err)
	}

	expectedTables := []string{
		"users", "workspaces", "workspace_members", "workspace_invitations",
		"shifts", "shift_aliases", "schedule_days", "schedule_segments",
		"schedule_revisions", "import_jobs", "import_items", "import_files",
		"auth_refresh_tokens", "audit_logs", "user_preferences",
	}
	for _, table := range expectedTables {
		var count int
		err := db.QueryRowContext(context.Background(), `
SELECT COUNT(*) FROM information_schema.tables
WHERE table_schema = DATABASE() AND table_name = ?`, table).Scan(&count)
		if err != nil {
			t.Fatalf("inspect table %s: %v", table, err)
		}
		if count != 1 {
			t.Errorf("expected table %s to exist", table)
		}
	}

	var uniqueCount int
	err = db.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = 'schedule_days'
  AND index_name = 'uq_schedule_day' AND non_unique = 0`).Scan(&uniqueCount)
	if err != nil {
		t.Fatalf("inspect schedule unique index: %v", err)
	}
	if uniqueCount != 3 {
		t.Fatalf("expected three columns in uq_schedule_day, got %d", uniqueCount)
	}
}

func ensureTestDatabase(t *testing.T, dsn string) {
	t.Helper()
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	databaseName := cfg.DBName
	if databaseName != "shiftory_test" {
		t.Fatalf("refusing to create unexpected test database %q", databaseName)
	}
	cfg.DBName = ""
	admin, err := sql.Open("mysql", cfg.FormatDSN())
	if err != nil {
		t.Fatalf("open mysql admin connection: %v", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if _, err := admin.ExecContext(context.Background(), fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_ai_ci", databaseName)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
}
