package database

import (
	"database/sql"
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Migrate 同步业务结构与更新时间触发器，不创建默认账号或删除业务数据
func Migrate(db *sql.DB) error {
	orm, err := gorm.Open(postgres.New(postgres.Config{Conn: db}), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return fmt.Errorf("initialize schema migrator: %w", err)
	}
	if err := orm.AutoMigrate(schemaModels()...); err != nil {
		return fmt.Errorf("auto migrate schema: %w", err)
	}
	if _, err := db.Exec(`CREATE OR REPLACE FUNCTION shiftory_touch_updated_at() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
   IF NEW IS DISTINCT FROM OLD THEN NEW.updated_at = statement_timestamp(); END IF;
   RETURN NEW;
 END;
 $$`); err != nil {
		return fmt.Errorf("create timestamp function: %w", err)
	}
	for _, table := range []string{"users", "workspaces", "workspace_members", "shifts", "import_jobs", "schedule_days", "import_items", "user_preferences"} {
		if _, err := db.Exec(`CREATE OR REPLACE TRIGGER trg_` + table + `_updated_at BEFORE UPDATE ON ` + table + ` FOR EACH ROW EXECUTE FUNCTION shiftory_touch_updated_at()`); err != nil {
			return fmt.Errorf("create timestamp trigger for %s: %w", table, err)
		}
	}
	return nil
}
