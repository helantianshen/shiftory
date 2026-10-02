package database

import (
	"database/sql"
	"fmt"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Migrate 使用现有连接池同步业务表、索引及约束，不创建默认账号或删除业务数据
func Migrate(db *sql.DB) error {
	orm, err := gorm.Open(mysql.New(mysql.Config{Conn: db}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		return fmt.Errorf("initialize schema migrator: %w", err)
	}
	if err := orm.Set("gorm:table_options", "ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_0900_as_cs").AutoMigrate(schemaModels()...); err != nil {
		return fmt.Errorf("auto migrate schema: %w", err)
	}
	return nil
}
