// Package main 提供一次性 GORM 表结构同步命令
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"shiftory-server/internal/importjob"
	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
	"shiftory-server/internal/platform/logging"
)

// main 使用与 API 相同的配置入口连接数据库并执行一次自动迁移，完成后退出
func main() {
	cfg, err := config.Load(os.Args[1:]...)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Environment == "production" {
		settings := cfg.Log
		extension := filepath.Ext(settings.File.Path)
		settings.File.Path = strings.TrimSuffix(settings.File.Path, extension) + ".migrate" + extension
		logger, logFile, err := logging.New(cfg.Environment, settings, nil)
		if err != nil {
			log.Fatal(err)
		}
		defer logFile.Close()
		slog.SetDefault(logger)
	}
	fail := func(err error) {
		if cfg.Environment == "production" {
			slog.Error("migration failed", "error", err)
			os.Exit(1)
		}
		log.Fatal(err)
	}
	db, err := database.Open(context.Background(), cfg.Postgres.DSN())
	if err != nil {
		fail(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		fail(err)
	}
	log.Println("Shiftory schema is up to date")
	if cfg.MigrateLegacyAI {
		n, err := importjob.MigrateLegacy(context.Background(), db, cfg.Tasks.MaxRounds)
		if err != nil {
			fail(errors.New("legacy AI migration failed"))
		}
		log.Printf("legacy AI jobs enqueued: %d", n)
	}
}
