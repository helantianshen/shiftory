// Package main 提供部署阶段的一次性数据库迁移命令
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"os"

	"shiftory-server/internal/platform/config"
	"shiftory-server/internal/platform/database"
)

// main 使用与 API 相同的配置入口连接数据库并执行一次迁移，完成后退出
func main() {
	cfg, err := config.Load(os.Args[1:]...)
	if errors.Is(err, flag.ErrHelp) {
		return
	}
	if err != nil {
		log.Fatal(err)
	}
	db, err := database.Open(context.Background(), cfg.MySQL.DSN())
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		log.Fatal(err)
	}
	log.Println("Shiftory migrations are up to date")
}
