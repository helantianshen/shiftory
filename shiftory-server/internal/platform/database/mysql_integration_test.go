package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"reflect"
	"shiftory-server/internal/auth"
	"sort"
	"strings"
	"testing"

	"github.com/go-sql-driver/mysql"
	"shiftory-server/internal/testutil"
)

// TestMigrateCreatesCompleteSchema 检查迁移幂等性、业务表及 uq_schedule_day_user_date 两列唯一索引断言
func TestMigrateCreatesCompleteSchema(t *testing.T) {
	dsn := testutil.MySQLDSN(t, "shiftory_test")
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

	var connectionCollation string
	if err := db.QueryRow("SELECT @@collation_connection").Scan(&connectionCollation); err != nil || connectionCollation != "utf8mb4_0900_as_cs" {
		t.Fatalf("unexpected connection collation: %s, %v", connectionCollation, err)
	}

	var uniqueCount int
	err = db.QueryRowContext(context.Background(), `
SELECT COUNT(*)
FROM information_schema.statistics
WHERE table_schema = DATABASE()
  AND table_name = 'schedule_days'
  AND index_name = 'uq_schedule_day_user_date' AND non_unique = 0`).Scan(&uniqueCount)
	if err != nil {
		t.Fatalf("inspect schedule unique index: %v", err)
	}
	if uniqueCount != 2 {
		t.Fatalf("expected two columns in uq_schedule_day_user_date, got %d", uniqueCount)
	}
}

// TestInitializationSQL 验证空库脚本、初始密码以及后续自动迁移不会改变字段或覆盖账号
func TestInitializationSQL(t *testing.T) {
	dsn := testutil.MySQLDSN(t, "shiftory_test")
	ensureTestDatabase(t, dsn)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	generated := schemaSnapshot(t, db)
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	ensureTestDatabase(t, dsn)
	db, err = sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	script, err := os.ReadFile("../../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	// 将脚本中的业务库名替换为固定测试库，并在同一连接上验证建库和选库
	if strings.Count(string(script), "`shiftory`") != 2 {
		t.Fatal("unexpected initialization database declarations")
	}
	testScript := strings.ReplaceAll(string(script), "`shiftory`", "`shiftory_test`")
	conn, err := db.Conn(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.ExecContext(context.Background(), "DROP DATABASE `shiftory_test`"); err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(testScript, ";") {
		if strings.TrimSpace(statement) == "" {
			continue
		}
		if _, err := conn.ExecContext(context.Background(), statement); err != nil {
			t.Fatalf("initialize SQL: %v", err)
		}
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	if !reflect.DeepEqual(generated, before) {
		for _, column := range generated {
			if !containsSchema(before, column) {
				t.Errorf("only in models: %s", column)
			}
		}
		for _, column := range before {
			if !containsSchema(generated, column) {
				t.Errorf("only in SQL: %s", column)
			}
		}
		t.FailNow()
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if after := schemaSnapshot(t, db); !reflect.DeepEqual(before, after) {
		t.Fatal("automatic migration changed initialized schema")
	}

	var hash string
	if err := db.QueryRow("SELECT password_hash FROM users WHERE username_normalized = 'admin'").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.NewPasswordHasher(auth.DefaultPasswordParams()).Verify(hash, "ShiftoryDev123!"); err != nil || !ok {
		t.Fatalf("initial password invalid: %v", err)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM workspace_members WHERE user_id=1 AND workspace_id=1 AND role='OWNER'").Scan(&count); err != nil || count != 1 {
		t.Fatalf("initial owner missing: %v", err)
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM shifts WHERE workspace_id=1").Scan(&count); err != nil || count != 3 {
		t.Fatalf("initial shifts missing: %v", err)
	}
	// 数据库比较区分大小写和重音，应用层仍负责规范化业务键
	if _, err := db.Exec("INSERT INTO shift_aliases (workspace_id, shift_id, alias, alias_normalized) VALUES (1, 1, 'cafe', 'cafe'), (1, 1, 'café', 'café'), (1, 1, 'CAFE', 'CAFE')"); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"cafe", "café", "CAFE"} {
		if err := db.QueryRow("SELECT COUNT(*) FROM shift_aliases WHERE alias_normalized = ?", alias).Scan(&count); err != nil || count != 1 {
			t.Fatalf("case/accent-sensitive lookup failed for %s: count=%d err=%v", alias, count, err)
		}
	}
	if _, err := db.Exec("INSERT INTO shift_aliases (workspace_id, shift_id, alias, alias_normalized) VALUES (1, 1, 'cafe', 'cafe')"); err == nil {
		t.Fatal("exact duplicate must be rejected")
	} else if mysqlErr, ok := err.(*mysql.MySQLError); !ok || mysqlErr.Number != 1062 {
		t.Fatalf("unexpected alias error: %v", err)
	}
	var mismatched int
	if err := db.QueryRow("SELECT COUNT(*) FROM information_schema.columns WHERE table_schema=DATABASE() AND collation_name IS NOT NULL AND collation_name <> 'utf8mb4_0900_as_cs'").Scan(&mismatched); err != nil || mismatched != 0 {
		t.Fatalf("unexpected column collation: count=%d err=%v", mismatched, err)
	}
}

// schemaSnapshot 读取字段、索引和约束以比较初始化 SQL 与自动迁移的一致性
func schemaSnapshot(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT CONCAT(TABLE_NAME, '.', COLUMN_NAME, ':', COLUMN_TYPE, ':', IS_NULLABLE, ':', COALESCE(COLUMN_DEFAULT, '<NULL>'), ':', EXTRA, ':', COALESCE(COLLATION_NAME, '<NULL>')) FROM information_schema.columns WHERE table_schema=DATABASE() ORDER BY table_name, ordinal_position`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var result []string
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatal(err)
		}
		result = append(result, column)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	rows.Close()
	// 索引与外键规则也必须一致，不能仅凭字段存在判断结构完整
	for _, query := range []string{
		`SELECT CONCAT('index:', TABLE_NAME, ':', INDEX_NAME, ':', NON_UNIQUE, ':', SEQ_IN_INDEX, ':', COLUMN_NAME) FROM information_schema.statistics WHERE table_schema=DATABASE()`,
		`SELECT CONCAT('fk:', TABLE_NAME, ':', CONSTRAINT_NAME, ':', REFERENCED_TABLE_NAME, ':', UPDATE_RULE, ':', DELETE_RULE) FROM information_schema.referential_constraints WHERE constraint_schema=DATABASE()`,
		`SELECT CONCAT('check:', CONSTRAINT_NAME, ':', CHECK_CLAUSE) FROM information_schema.check_constraints WHERE constraint_schema=DATABASE()`,
	} {
		constraintRows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for constraintRows.Next() {
			var item string
			if err := constraintRows.Scan(&item); err != nil {
				constraintRows.Close()
				t.Fatal(err)
			}
			result = append(result, item)
		}
		err = constraintRows.Err()
		constraintRows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(result)
	return result
}

// containsSchema 检查结构快照中是否存在指定定义
func containsSchema(items []string, value string) bool {
	index := sort.SearchStrings(items, value)
	return index < len(items) && items[index] == value
}

// ensureTestDatabase 仅允许重建 shiftory_test 测试库，拒绝对其他库执行初始化
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
	if _, err := admin.Exec("DROP DATABASE IF EXISTS `shiftory_test`"); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.ExecContext(context.Background(), fmt.Sprintf(
		"CREATE DATABASE IF NOT EXISTS `%s` CHARACTER SET utf8mb4 COLLATE utf8mb4_0900_as_cs", databaseName)); err != nil {
		t.Fatalf("create test database: %v", err)
	}
}
