// Package testutil 提供集成测试使用的隔离数据库连接参数
package testutil

import (
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
)

// PostgresDSN 只允许访问项目专用测试库，连接失败不能静默跳过测试
func PostgresDSN(t *testing.T, database string) string {
	t.Helper()
	switch database {
	case "shiftory_test", "shiftory_test_httpserver", "shiftory_test_importjob":
	default:
		t.Fatalf("unexpected test database %q", database)
	}
	raw := os.Getenv("SHIFTORY_TEST_DATABASE_DSN")
	if raw == "" {
		raw = "postgres://shiftory_test:123456@127.0.0.1:5432/shiftory_test?sslmode=disable"
	}
	if _, err := pgx.ParseConfig(raw); err != nil {
		t.Fatal("invalid test database connection configuration")
	}
	value, err := url.Parse(raw)
	if err != nil || (value.Scheme != "postgres" && value.Scheme != "postgresql") {
		t.Fatal("test database DSN must be a PostgreSQL URL")
	}
	value.Path, value.RawPath = "/"+database, ""
	params := value.Query()
	params.Set("timezone", "UTC")
	value.RawQuery = params.Encode()
	return value.String()
}
