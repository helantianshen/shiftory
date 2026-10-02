// Package testutil 提供集成测试使用的隔离数据库连接参数
package testutil

import (
	"os"
	"testing"

	"github.com/go-sql-driver/mysql"
)

// MySQLDSN 复用测试连接凭据，但只允许连接服务器管理库或项目专用测试库
func MySQLDSN(t *testing.T, database string) string {
	t.Helper()
	switch database {
	case "mysql", "shiftory_test", "shiftory_test_httpserver", "shiftory_test_importjob":
	default:
		t.Fatalf("unexpected test database %q", database)
	}
	raw := os.Getenv("SHIFTORY_TEST_DATABASE_DSN")
	if raw == "" {
		raw = "root:123456@tcp(127.0.0.1:3306)/shiftory_test?charset=utf8mb4&parseTime=true&loc=UTC"
	}
	cfg, err := mysql.ParseDSN(raw)
	if err != nil {
		t.Fatal("invalid test database connection configuration")
	}
	cfg.DBName = database
	cfg.Collation = ""
	if cfg.Params == nil {
		cfg.Params = map[string]string{}
	}
	cfg.Params["collation_connection"] = "'utf8mb4_0900_as_cs'"
	return cfg.FormatDSN()
}
