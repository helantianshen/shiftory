package database

import (
	"database/sql"
	"fmt"

	"github.com/pressly/goose/v3"

	"shiftory-server/migrations"
)

// Migrate 配置内嵌迁移文件并执行待应用的 Goose Up 迁移
func Migrate(db *sql.DB) error {
	goose.SetBaseFS(migrations.Files)
	if err := goose.SetDialect("mysql"); err != nil {
		return fmt.Errorf("set goose dialect: %w", err)
	}
	if err := goose.Up(db, "."); err != nil {
		return fmt.Errorf("run goose migrations: %w", err)
	}
	return nil
}
