package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"shiftory-server/internal/auth"
	"shiftory-server/internal/testutil"
)

func TestMigrateCreatesCompleteSchema(t *testing.T) {
	dsn := testutil.PostgresDSN(t, "shiftory_test")
	ensureTestDatabase(t, dsn)
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	before := schemaSnapshot(t, db)
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, schemaSnapshot(t, db)) {
		t.Fatal("repeated migration changed schema")
	}
	tables := []string{"users", "workspaces", "workspace_members", "workspace_invitations", "shifts", "shift_aliases", "schedule_days", "schedule_segments", "schedule_revisions", "import_jobs", "import_items", "import_files", "auth_refresh_tokens", "audit_logs", "user_preferences", "import_outbox", "import_attempts", "import_provider_calls"}
	for _, table := range tables {
		var count int
		if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=$1`, table).Scan(&count); err != nil || count != 1 {
			t.Fatalf("missing table %s: %v", table, err)
		}
	}
	var zone, index string
	if err := db.QueryRow("SHOW timezone").Scan(&zone); err != nil || zone != "UTC" {
		t.Fatalf("unexpected timezone %s: %v", zone, err)
	}
	if err := db.QueryRow(`SELECT pg_get_indexdef(indexrelid) FROM pg_index WHERE indexrelid='uq_schedule_day_user_date'::regclass AND indisunique`).Scan(&index); err != nil {
		t.Fatal(err)
	}
	if index != "CREATE UNIQUE INDEX uq_schedule_day_user_date ON public.schedule_days USING btree (user_id, work_date)" {
		t.Fatalf("unexpected unique index: %s", index)
	}
}

func TestInitializationSQL(t *testing.T) {
	dsn := testutil.PostgresDSN(t, "shiftory_test")
	ensureTestDatabase(t, dsn)
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	generated := schemaSnapshot(t, db)
	db.Close()
	ensureTestDatabase(t, dsn)
	db, err = Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	script, err := os.ReadFile("../../../sql/init.sql")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(string(script)); err != nil {
		t.Fatalf("initialize SQL: %v", err)
	}
	before := schemaSnapshot(t, db)
	if !reflect.DeepEqual(generated, before) {
		for _, item := range generated {
			if !containsSchema(before, item) {
				t.Errorf("only in models: %s", item)
			}
		}
		for _, item := range before {
			if !containsSchema(generated, item) {
				t.Errorf("only in SQL: %s", item)
			}
		}
		t.FailNow()
	}
	if err := Migrate(db); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, schemaSnapshot(t, db)) {
		t.Fatal("migration changed initialized schema")
	}
	var hash string
	if err := db.QueryRow("SELECT password_hash FROM users WHERE username_normalized='admin'").Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if ok, err := auth.NewPasswordHasher(auth.DefaultPasswordParams()).Verify(hash, "ShiftoryDev123!"); err != nil || !ok {
		t.Fatalf("invalid initial password: %v", err)
	}
	var count int
	for _, query := range []string{"SELECT COUNT(*) FROM workspace_members WHERE user_id=1 AND workspace_id=1 AND role='OWNER'", "SELECT COUNT(*) FROM shifts WHERE workspace_id=1"} {
		if err := db.QueryRow(query).Scan(&count); err != nil || count == 0 {
			t.Fatalf("initial seed missing: %v", err)
		}
	}
	if _, err := db.Exec("INSERT INTO shift_aliases (workspace_id,shift_id,alias,alias_normalized) VALUES (1,1,'cafe','cafe'),(1,1,'café','café'),(1,1,'CAFE','CAFE')"); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"cafe", "café", "CAFE"} {
		if err := db.QueryRow("SELECT COUNT(*) FROM shift_aliases WHERE alias_normalized=$1", alias).Scan(&count); err != nil || count != 1 {
			t.Fatalf("case/accent lookup failed: %s count=%d err=%v", alias, count, err)
		}
	}
	_, err = db.Exec("INSERT INTO shift_aliases (workspace_id,shift_id,alias,alias_normalized) VALUES (1,1,'cafe','cafe')")
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate not rejected: %v", err)
	}
	// 固定种子 ID 不能使后续新增账号与工作区发生序列冲突
	var id int64
	if err := db.QueryRow("INSERT INTO users (username,username_normalized,email,email_normalized,display_name,password_hash) VALUES ('next','next','next@example.com','next@example.com','Next','hash') RETURNING id").Scan(&id); err != nil || id <= 1 {
		t.Fatalf("user sequence invalid: id=%d err=%v", id, err)
	}
	if err := db.QueryRow("INSERT INTO workspaces (name,owner_user_id,created_by) VALUES ('Next', $1,$1) RETURNING id", id).Scan(&id); err != nil || id <= 1 {
		t.Fatalf("workspace sequence invalid: id=%d err=%v", id, err)
	}
	var old, updated time.Time
	if err := db.QueryRow("SELECT updated_at FROM users WHERE id=1").Scan(&old); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("UPDATE users SET display_name='Changed' WHERE id=1 RETURNING updated_at").Scan(&updated); err != nil || !updated.After(old) {
		t.Fatalf("updated_at did not advance: %v", err)
	}
	if _, err := db.Exec("INSERT INTO auth_refresh_tokens(user_id,family_id,jwt_id,token_hash,expires_at) VALUES (1,'bad','bad',decode('00','hex'),CURRENT_TIMESTAMP)"); err == nil {
		t.Fatal("invalid token hash length accepted")
	}
	if _, err := db.Exec("INSERT INTO users(id,username,username_normalized,email,email_normalized,display_name,password_hash) VALUES (-1,'negative','negative','neg@example.com','neg@example.com','Negative','hash')"); err == nil {
		t.Fatal("negative ID accepted")
	}
}

// schemaSnapshot 比较字段、索引、约束及触发器的实际数据库定义
func schemaSnapshot(t *testing.T, db *sql.DB) []string {
	t.Helper()
	queries := []string{
		`SELECT concat('column:',c.relname,':',a.attname,':',format_type(a.atttypid,a.atttypmod),':',a.attnotnull,':',coalesce(pg_get_expr(d.adbin,d.adrelid),'<NULL>'),':',coalesce(coll.collname,'<NULL>')) FROM pg_attribute a JOIN pg_class c ON c.oid=a.attrelid JOIN pg_namespace n ON n.oid=c.relnamespace LEFT JOIN pg_attrdef d ON d.adrelid=a.attrelid AND d.adnum=a.attnum LEFT JOIN pg_collation coll ON coll.oid=a.attcollation WHERE n.nspname='public' AND c.relkind='r' AND a.attnum>0 AND NOT a.attisdropped`,
		`SELECT concat('index:',tablename,':',indexname,':',indexdef) FROM pg_indexes WHERE schemaname='public'`,
		`SELECT concat('constraint:',c.relname,':',con.conname,':',pg_get_constraintdef(con.oid)) FROM pg_constraint con JOIN pg_class c ON c.oid=con.conrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public'`,
		`SELECT concat('trigger:',pg_get_triggerdef(t.oid)) FROM pg_trigger t JOIN pg_class c ON c.oid=t.tgrelid JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname='public' AND NOT t.tgisinternal`,
		`SELECT concat('function:',pg_get_functiondef(p.oid)) FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace WHERE n.nspname='public'`,
	}
	var result []string
	for _, query := range queries {
		rows, err := db.Query(query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var item string
			if err := rows.Scan(&item); err != nil {
				rows.Close()
				t.Fatal(err)
			}
			result = append(result, item)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(result)
	return result
}
func containsSchema(items []string, value string) bool {
	i := sort.SearchStrings(items, value)
	return i < len(items) && items[i] == value
}

// ensureTestDatabase 只重建固定测试库的 public schema，不删除共享实例的其他数据库
func ensureTestDatabase(t *testing.T, dsn string) {
	t.Helper()
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil || cfg.Database != "shiftory_test" {
		t.Fatal("refusing unexpected test database")
	}
	db, err := Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
}
